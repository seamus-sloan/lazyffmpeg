package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
)

// finishedRun is a 100x30 main screen whose run of s has just finished,
// opening files with open.
func finishedRun(t *testing.T, s app.Session, open OpenFunc) Model {
	t.Helper()
	done := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		return runner.Result{Output: job.Output, Size: 1000}, nil
	}
	m := New(s, WithRunner(done), WithOpener(open))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("r"))
	if m.run.phase != runDone {
		t.Fatalf("run.phase = %v, want runDone", m.run.phase)
	}
	return m
}

func TestOOpensAFinishedRunsOutput(t *testing.T) {
	dir := t.TempDir()
	for _, input := range []string{"clip.mov", "photo.jpg"} {
		s := testSession(pipeline.New())
		if input == "photo.jpg" {
			s = imageSession(pipeline.New())
		}
		s.Input = filepath.Join(dir, input)
		var opened []string
		m := finishedRun(t, s, func(path string) error {
			opened = append(opened, path)
			return nil
		})
		if out := viewText(m); !strings.Contains(out, "o open it · press any key to continue") {
			t.Errorf("%s: done screen does not offer to open the output, got:\n%s", input, out)
		}

		m = step(t, m, key("o"))

		want := s.OutputPath(s.Pipeline)
		if len(opened) != 1 || opened[0] != want {
			t.Errorf("%s: opened %q, want %q", input, opened, want)
		}
		if m.run.phase != runNone {
			t.Errorf("%s: run.phase = %v, want the done screen closed", input, m.run.phase)
		}
		if out := viewText(m); !strings.Contains(out, "opened "+filepath.Base(want)) {
			t.Errorf("%s: footer does not say it opened the output, got:\n%s", input, out)
		}
	}
}

func TestOReportsWhenTheOutputCannotBeOpened(t *testing.T) {
	s := testSession(pipeline.New())
	s.Input = filepath.Join(t.TempDir(), "clip.mov")
	m := finishedRun(t, s, func(string) error { return errors.New("no opener") })

	m = step(t, m, key("o"))

	if out := viewText(m); !strings.Contains(out, "failed: no opener") {
		t.Errorf("footer does not report the failure, got:\n%s", out)
	}
}

func TestOtherKeysCloseTheDoneScreenWithoutOpening(t *testing.T) {
	s := testSession(pipeline.New())
	s.Input = filepath.Join(t.TempDir(), "clip.mov")
	opened := 0
	m := finishedRun(t, s, func(string) error { opened++; return nil })

	m = step(t, m, key("x"))

	if opened != 0 || m.run.phase != runNone {
		t.Errorf("opened %d times, run.phase %v; want the screen closed and nothing opened", opened, m.run.phase)
	}
}

func TestYCopiesAnImage(t *testing.T) {
	fn, reqs := fakeRenderer("⣿", nil)
	clip, got := fakeClipboard(nil)
	m := New(imageSession(pipeline.New()), WithRenderer(fn, true), WithClipboard(clip))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m, req := copied(t, m, reqs)

	if req.Time != 0 || req.Input != "photo.jpg" || len(*got) != 1 {
		t.Errorf("request %+v, clipboard %q; want the image's one frame copied", req, *got)
	}
	if !strings.Contains(viewText(m), "copied the image as 100×1 symbols") {
		t.Errorf("footer does not confirm the copy, got:\n%s", viewText(m))
	}
}
