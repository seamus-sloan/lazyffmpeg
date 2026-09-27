package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// RunFunc runs one ffmpeg job, reporting progress as it goes. The default
// is runner.Run; tests inject a fake via WithRunner.
type RunFunc func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error)

// WithRunner overrides the function used to run a compiled pipeline.
func WithRunner(fn RunFunc) Option {
	return func(m *Model) { m.runFn = fn }
}

// runPhase names which run-related screen, if any, is showing over the
// main editor.
type runPhase int

const (
	runNone runPhase = iota
	runConfirmOverwrite
	runRunning
	runDone
	runError
)

// runState is the run flow's state, active whenever run.phase != runNone.
type runState struct {
	phase runPhase

	job runner.Job
	// inputSize is the input's size when the run started: the done
	// screen's "before" size, which must survive the reprobe that follows
	// an --in-place run replacing session.Info.
	inputSize int64

	progress runner.Progress
	result   runner.Result
	err      error
	canceled bool

	pendingCancel bool
	pendingQuit   bool
	quitAfterRun  bool // set once quit is confirmed; tea.Quit fires from handleRunDone

	cancel context.CancelFunc
	msgs   chan tea.Msg
}

// runProgressMsg carries one live progress update from a run in progress.
type runProgressMsg runner.Progress

// runDoneMsg carries a run's final outcome.
type runDoneMsg struct {
	result runner.Result
	err    error
}

// runReprobedMsg carries the outcome of re-probing the input after a
// successful --in-place run: the file on disk has changed underneath the
// session, so its probed Info (duration, size, ...) needs refreshing.
type runReprobedMsg struct {
	info probe.Info
	err  error
}

// reprobeCmd re-probes the session's input, for after a successful
// --in-place run.
func (m Model) reprobeCmd() tea.Cmd {
	fn := m.probeFn
	ctx := m.ctx
	path := m.session.Input
	return func() tea.Msg {
		info, err := fn(ctx, path)
		return runReprobedMsg{info: info, err: err}
	}
}

// handleRunReprobed applies a successful re-probe's fresh Info to the
// session and re-renders the preview from the rewritten file, with its
// position clamped into the file's new length; a failed re-probe leaves
// the (now stale, but still usable) existing Info in place.
func (m Model) handleRunReprobed(msg runReprobedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	m.session.Info = msg.info
	m = m.clampPreviewTime()
	return m.requestRender()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tryRun compiles the current pipeline and either starts the run directly,
// asks for an overwrite confirmation first, or refuses outright when the
// resolved output is the input file without --in-place (mirroring the
// headless path's output-safety rule).
func (m Model) tryRun() (Model, tea.Cmd) {
	outputPath := m.session.OutputPath(m.pipeline)
	opts := pipeline.Options{Input: m.session.Input, Output: outputPath}

	argv, err := pipeline.Compile(m.session.Info, m.pipeline, opts)
	if err != nil {
		return m, nil // the error already shows in the footer
	}

	if !m.session.InPlace && app.SameAsInput(m.session.Input, outputPath) {
		m.run = runState{phase: runError, err: app.ErrSameAsInput}
		return m, nil
	}

	if m.session.InPlace {
		if err := app.CheckInPlace(m.session.Input, m.pipeline); err != nil {
			m.run = runState{phase: runError, err: err}
			return m, nil
		}
	}

	dur := pipeline.OutputDuration(m.session.Info, m.pipeline)
	job := runner.Job{Argv: argv, Output: outputPath, Duration: dur}

	needsConfirm := m.session.InPlace || fileExists(outputPath)
	if needsConfirm && !m.session.Force {
		m.run = runState{phase: runConfirmOverwrite, job: job}
		return m, nil
	}

	return m.startRun(job)
}

func (m Model) startRun(job runner.Job) (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 16)

	m.run = runState{phase: runRunning, job: job, inputSize: m.session.Info.SizeBytes, cancel: cancel, msgs: ch}
	if m.preview.playing {
		m.preview.playing = false
		m.preview.playGen++ // invalidate any tick still ticking down from before the run
	}

	fn := m.runFn
	wg := m.runWG
	programCtx := m.ctx
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		onProgress := func(p runner.Progress) {
			select {
			case ch <- runProgressMsg(p):
			case <-ctx.Done():
			}
		}
		result, err := fn(ctx, job, onProgress)
		// fn has returned with its cleanup (e.g. removing a canceled run's
		// temp file) done, which is all Run waits for: it must not also
		// wait on the outcome being read, since after the program exits
		// nothing reads it.
		if wg != nil {
			wg.Done()
		}
		select {
		case ch <- runDoneMsg{result: result, err: err}:
		case <-programCtx.Done(): // the program is exiting; nobody will read it
		}
	}()

	return m, waitForRunMsg(ch)
}

func waitForRunMsg(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m Model) handleRunProgress(msg runProgressMsg) (tea.Model, tea.Cmd) {
	if m.run.phase != runRunning {
		return m, nil
	}
	rs := m.run
	rs.progress = runner.Progress(msg)
	m.run = rs
	return m, waitForRunMsg(rs.msgs)
}

// handleRunDone reaches a run's final outcome. When a quit was confirmed
// while the run was in progress (runState.quitAfterRun), tea.Quit fires
// only here — once the run (and its cleanup, e.g. removing a canceled
// run's temp file) has actually finished — never from the confirmation
// keypress itself.
func (m Model) handleRunDone(msg runDoneMsg) (tea.Model, tea.Cmd) {
	rs := m.run
	quit := rs.quitAfterRun
	succeeded := msg.err == nil
	if msg.err != nil {
		if errors.Is(msg.err, runner.ErrCanceled) {
			rs.phase = runDone
			rs.canceled = true
		} else {
			rs.phase = runError
			rs.err = msg.err
		}
	} else {
		rs.phase = runDone
		rs.result = msg.result
	}
	m.run = rs

	if quit {
		m.quitting = true
		return m, tea.Quit
	}
	if succeeded && m.session.InPlace {
		// The file on disk changed underneath the session: re-probe it
		// and drop undo history, which no longer applies to whatever the
		// new file now contains.
		m.undo = nil
		return m, m.reprobeCmd()
	}
	return m, nil
}

func (m Model) handleRunKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.run.phase {
	case runConfirmOverwrite:
		return m.handleConfirmOverwriteKey(msg)
	case runRunning:
		return m.handleRunningKey(msg)
	case runDone, runError:
		m.run = runState{}
		return m, nil
	}
	return m, nil
}

func (m Model) handleConfirmOverwriteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		return m.startRun(m.run.job)
	case "n", "esc":
		m.run = runState{}
		return m, nil
	}
	return m, nil
}

func (m Model) handleRunningKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	rs := m.run

	if rs.pendingCancel {
		switch k {
		case "y":
			if rs.cancel != nil {
				rs.cancel()
			}
			rs.pendingCancel = false
			m.run = rs
		case "n", "esc":
			rs.pendingCancel = false
			m.run = rs
		}
		return m, nil
	}

	if rs.pendingQuit {
		switch k {
		case "y":
			if rs.cancel != nil {
				rs.cancel()
			}
			rs.pendingQuit = false
			rs.quitAfterRun = true
			m.run = rs
		case "n", "esc":
			rs.pendingQuit = false
			m.run = rs
		}
		return m, nil
	}

	switch k {
	case "esc":
		rs.pendingCancel = true
		m.run = rs
	case "q", "ctrl+c":
		rs.pendingQuit = true
		m.run = rs
	}
	return m, nil
}

func (m Model) runBodyLines() []string {
	rs := m.run
	switch rs.phase {
	case runConfirmOverwrite:
		prompt := fmt.Sprintf("Overwrite %s? (y/n)", rs.job.Output)
		if m.session.InPlace {
			prompt = fmt.Sprintf("Replace original %s? (y/n)", rs.job.Output)
		}
		return []string{prompt}

	case runRunning:
		width := m.innerWidth() - 2
		if width < 10 {
			width = 10
		}
		bar := progress.New(progress.WithWidth(width))
		pct := rs.progress.Percent

		lines := []string{
			bar.ViewAs(pct / 100),
			fmt.Sprintf("%.0f%%  elapsed %s  eta %s  speed %.1fx",
				pct, rs.progress.Elapsed.Round(time.Second), rs.progress.ETA.Round(time.Second), rs.progress.Speed),
		}
		if rs.pendingCancel {
			lines = append(lines, "", "Cancel encoding? (y/n)")
		}
		if rs.pendingQuit {
			lines = append(lines, "", "Quit and cancel encoding? (y/n)")
		}
		if rs.quitAfterRun {
			lines = append(lines, "", "canceling…")
		}
		return lines

	case runDone:
		if rs.canceled {
			return []string{"Canceled", "", "press any key to continue"}
		}
		return []string{
			fmt.Sprintf("Wrote %s", rs.result.Output),
			units.FormatSizeChange(rs.inputSize, rs.result.Size),
			"",
			"press any key to continue",
		}

	case runError:
		var exitErr *runner.ExitError
		if !errors.As(rs.err, &exitErr) {
			return []string{"Error: " + rs.err.Error(), "", "press any key to continue"}
		}
		// Show as much of the end of ffmpeg's stderr as fits between the
		// heading and the two closing lines inside the frame's borders.
		tail := exitErr.Tail
		room := maxInt(m.height-2-3, 0)
		if len(tail) > room {
			tail = tail[len(tail)-room:]
		}
		lines := append([]string{"ffmpeg failed:"}, tail...)
		return append(lines, "", "press any key to continue")
	}
	return nil
}

func (m Model) renderRunFrame() string {
	width := m.width
	top := frameTop(width, m.frameTitle())
	bottom := frameBottom(width)

	var lines []string
	lines = append(lines, top)
	inner := m.innerWidth()
	for _, l := range m.runBodyLines() {
		lines = append(lines, sideLine(width, padOrTruncate(l, inner)))
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}
