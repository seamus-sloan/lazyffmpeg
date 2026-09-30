package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/picker"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
)

// assertFits fails t when view has more lines than height, any line
// wider than width terminal cells, or any line that is not a row of the
// frame (starting and ending on its border), as happens when text holding
// a newline is drawn into what should be a single row.
func assertFits(t *testing.T, name, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Errorf("%s: %d lines in a %dx%d terminal:\n%s", name, len(lines), width, height, view)
		return
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("%s: line %d is %d cells wide in a %dx%d terminal: %q", name, i+1, w, width, height, l)
			return
		}
		r := []rune(l)
		if len(r) == 0 || !strings.ContainsRune("╭│╰", r[0]) || !strings.ContainsRune("╮│╯", r[len(r)-1]) {
			t.Errorf("%s: line %d is not a frame row: %q", name, i+1, l)
			return
		}
	}
}

// layoutSteps is allKindSteps with a very long raw-args step, so the
// compiled command wraps across many footer lines once expanded, and a
// File name close to the longest allowed, so the footer's output path and
// the PIPELINE row run long too.
func layoutSteps() []pipeline.Step {
	steps := allKindSteps()
	long := strings.Repeat("y", 400)
	for i, s := range steps {
		switch s.Kind() {
		case pipeline.KindFilename:
			steps[i] = pipeline.Filename{Name: strings.Repeat("a long output name ", 12) + ".mkv"}
		case pipeline.KindRawArgs:
			steps[i] = pipeline.RawArgs{Text: long, Args: []string{long}}
		}
	}
	return steps
}

// multiLineError is error text the way ffmpeg and ffprobe report it: many
// lines of stderr, long ones among them, ending in the line that matters
// and a trailing blank line.
func multiLineError() string {
	return strings.Repeat("ffprobe: noise from stderr that is long enough to need truncating on its own\n", 30) +
		"clip.mov: Invalid data found when processing input\n\n"
}

// screensOf returns every screen the main editor can show for base: the
// editor itself with either panel focused, the help and a step modal (a
// preset one, and File name's prefilled input with its hint) over it, and
// each run-flow screen, including ones showing multi-line errors.
func screensOf(base Model) map[string]Model {
	screens := map[string]Model{"main": base}

	previewErr := base
	previewErr.preview.frameErr = multiLineError()
	screens["main, multi-line preview error"] = previewErr

	mm, _ := base.Update(key("tab"))
	pipelineFocus := mm.(Model)
	pipelineFocus.pipelineCursor = maxInt(pipelineFocus.pipeline.Len()-1, 0)
	screens["main, pipeline focus"] = pipelineFocus

	mm, _ = base.Update(key("?"))
	screens["help"] = mm.(Model)

	modal := base
	modal.focus = focusMenu
	modal.menuCursor = 0
	mm, _ = modal.Update(key("enter"))
	screens["modal"] = mm.(Model)
	modalErr := mm.(Model)
	ms := *modalErr.modal
	ms.err = multiLineError()
	modalErr.modal = &ms
	screens["modal, multi-line error"] = modalErr

	fileName := base
	fileName.focus = focusMenu
	for i, k := range fileName.menuKinds() {
		if k == pipeline.KindFilename {
			fileName.menuCursor = i
		}
	}
	mm, _ = fileName.Update(key("enter"))
	screens["file name modal"] = mm.(Model)

	tail := make([]string, 40)
	for i := range tail {
		tail[i] = fmt.Sprintf("ffmpeg stderr line %d", i+1)
	}
	longOut := "/out/" + strings.Repeat("o", 300) + ".mp4"
	runs := map[string]runState{
		"confirm":            {phase: runConfirmOverwrite, job: runner.Job{Output: longOut}},
		"running":            {phase: runRunning, progress: runner.Progress{Percent: 42, Speed: 1.5, Elapsed: 3 * time.Second, ETA: 4 * time.Second}, pendingQuit: true},
		"done":               {phase: runDone, result: runner.Result{Output: longOut, Size: 1_000_000}},
		"error":              {phase: runError, err: &runner.ExitError{Code: 1, Tail: tail}},
		"failure":            {phase: runError, err: errors.New(strings.Repeat("bad ", 100))},
		"multi-line failure": {phase: runError, err: errors.New(multiLineError())},
		"multi-line confirm": {phase: runConfirmOverwrite, job: runner.Job{Output: "/out/odd\nname\n.mp4"}},
		"multi-line done":    {phase: runDone, result: runner.Result{Output: "/out/odd\nname\n.mp4", Size: 1}},
	}
	for name, rs := range runs {
		m := base
		m.run = rs
		screens["run "+name] = m
	}
	return screens
}

func TestEveryScreenFitsTheTerminal(t *testing.T) {
	sizes := [][2]int{{80, 24}, {100, 30}, {130, 40}}
	inputs := []string{"clip.mov", "/videos/" + strings.Repeat("a very long file name ", 15) + ".mov"}
	steps := layoutSteps()

	for _, size := range sizes {
		w, h := size[0], size[1]
		for _, input := range inputs {
			for n := 0; n <= len(steps); n++ {
				for _, expanded := range []bool{false, true} {
					s := testSession(pipeline.New(steps[:n]...))
					s.Input = input
					base := resized(New(s), w, h)
					if expanded {
						mm, _ := base.Update(key("c"))
						base = mm.(Model)
					}
					for name, m := range screensOf(base) {
						label := fmt.Sprintf("%dx%d, %d steps, c=%v, input %d chars, %s", w, h, n, expanded, len(input), name)
						assertFits(t, label, viewText(m), w, h)
					}
				}
			}
		}

		var entries []picker.Entry
		for i := 0; i < 60; i++ {
			entries = append(entries, picker.Entry{
				Name: fmt.Sprintf("%s clip %02d.mp4", strings.Repeat("long ", 30), i),
				Path: fmt.Sprintf("/dir/clip%02d.mp4", i), Size: int64(i) * 1000,
			})
		}
		longDir := "/" + strings.Repeat("nested/", 30) + "dir"
		for _, dir := range []string{"/dir", longDir} {
			m := New(app.Session{Dir: dir})
			mm, _ := m.Update(pickerListedMsg{dir: dir, entries: entries})
			m = resized(mm.(Model), w, h)
			m.picker.filtering = true
			m.picker.status = strings.Repeat("probe failed ", 20)
			assertFits(t, fmt.Sprintf("%dx%d picker in %d-char dir", w, h, len(dir)), viewText(m), w, h)
			m.picker.status = multiLineError()
			assertFits(t, fmt.Sprintf("%dx%d picker with a multi-line probe error", w, h), viewText(m), w, h)
			mm, _ = m.Update(key("?"))
			assertFits(t, fmt.Sprintf("%dx%d picker help", w, h), viewText(mm.(Model)), w, h)
		}
	}
}
