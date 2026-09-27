package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
)

// runningFake reports 50% progress, then blocks until either release is
// closed (success) or the run's context is canceled.
func runningFake(release chan struct{}) RunFunc {
	return func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		onProgress(runner.Progress{Percent: 50, Speed: 1.5})
		select {
		case <-release:
			return runner.Result{Output: job.Output, Size: 123}, nil
		case <-ctx.Done():
			return runner.Result{}, runner.ErrCanceled
		}
	}
}

func TestTryRunCallsRunFuncWithCompiledJob(t *testing.T) {
	release := make(chan struct{})
	var gotJob runner.Job
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		gotJob = job
		return runningFake(release)(ctx, job, onProgress)
	}

	pl := pipeline.New(pipeline.Speed{Factor: 2})
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Pipeline: pl, Output: outputPath}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("r issued no command")
	}
	cmd() // drains the first progress message so the fake has definitely run

	if calls != 1 {
		t.Fatalf("RunFunc called %d times, want 1", calls)
	}
	wantArgv, _ := pipeline.Compile(s.Info, pl, pipeline.Options{Input: s.Input, Output: outputPath})
	if !reflect.DeepEqual(gotJob.Argv, wantArgv) {
		t.Errorf("Job.Argv = %v, want %v", gotJob.Argv, wantArgv)
	}
	if gotJob.Output != outputPath {
		t.Errorf("Job.Output = %q, want %q", gotJob.Output, outputPath)
	}
	wantDur := pipeline.OutputDuration(s.Info, pl)
	if gotJob.Duration != wantDur {
		t.Errorf("Job.Duration = %v, want %v", gotJob.Duration, wantDur)
	}
	close(release)
}

func TestRunWithCompileErrorDoesNotRun(t *testing.T) {
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	}
	pl := pipeline.New(pipeline.Trim{Start: 40}) // starts past the 33s duration: invalid
	m := New(testSession(pl), WithRunner(fake))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil {
		t.Error("r with a compile error issued a run command")
	}
	if calls != 0 {
		t.Errorf("RunFunc called %d times, want 0", calls)
	}
	if m.run.phase != runNone {
		t.Errorf("run.phase = %v, want runNone", m.run.phase)
	}
	out := viewText(m)
	if !strings.Contains(out, "trim is outside the clip") {
		t.Errorf("compile error missing from the footer, got:\n%s", out)
	}
}

func TestTryRunRefusesSameAsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mov")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	}
	s := app.Session{Input: in, Info: testInfo(), Output: in}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, _ := m.Update(key("r"))
	m = mm.(Model)
	if calls != 0 {
		t.Errorf("RunFunc called %d times, want 0", calls)
	}
	if m.run.phase != runError || !errors.Is(m.run.err, app.ErrSameAsInput) {
		t.Fatalf("run = %+v, want a runError with ErrSameAsInput", m.run)
	}
	out := viewText(m)
	if !strings.Contains(out, app.ErrSameAsInput.Error()) {
		t.Errorf("view missing ErrSameAsInput message, got:\n%s", out)
	}
}

func TestInPlaceContainerMismatchDoesNotRun(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	}
	s := app.Session{Input: in, Info: testInfo(), InPlace: true, Force: true, Pipeline: pipeline.New(pipeline.Container{Format: pipeline.FormatMKV})}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("r issued a run command despite the in-place container mismatch")
	}
	if m.run.phase != runError {
		t.Fatalf("run.phase = %v, want runError", m.run.phase)
	}
	out := viewText(m)
	if !strings.Contains(out, "in-place") {
		t.Errorf("view missing the in-place container error, got:\n%s", out)
	}

	// The RunFunc must never have been invoked either (a real ffmpeg run
	// would have raced the check, but there is nothing to race here since
	// tryRun never starts one).
	if calls != 0 {
		t.Errorf("RunFunc called %d times, want 0", calls)
	}
}

func TestOverwriteConfirmPrompt(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{Output: job.Output}, nil
	}
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: outputPath}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil {
		t.Error("expected no run command yet (awaiting confirmation)")
	}
	if m.run.phase != runConfirmOverwrite {
		t.Fatalf("run.phase = %v, want runConfirmOverwrite", m.run.phase)
	}
	out := viewText(m)
	want := "Overwrite " + outputPath + "? (y/n)"
	if !strings.Contains(out, want) {
		t.Errorf("missing overwrite prompt %q, got:\n%s", want, out)
	}

	// n: no run.
	mm, _ = m.Update(key("n"))
	m = mm.(Model)
	if m.run.phase != runNone {
		t.Errorf("run.phase after n = %v, want runNone", m.run.phase)
	}
	if calls != 0 {
		t.Errorf("RunFunc called %d times after n, want 0", calls)
	}

	// r again, then y: runs.
	mm, _ = m.Update(key("r"))
	m = mm.(Model)
	mm, cmd = m.Update(key("y"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("y issued no run command")
	}
	cmd()
	if calls != 1 {
		t.Errorf("RunFunc called %d times after y, want 1", calls)
	}
}

func TestInPlaceConfirmPromptWording(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mov")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		return runner.Result{}, nil
	}
	s := app.Session{Input: in, Info: testInfo(), InPlace: true}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, _ := m.Update(key("r"))
	m = mm.(Model)
	if m.run.phase != runConfirmOverwrite {
		t.Fatalf("run.phase = %v, want runConfirmOverwrite", m.run.phase)
	}
	out := viewText(m)
	want := "Replace original " + in + "? (y/n)"
	if !strings.Contains(out, want) {
		t.Errorf("missing in-place prompt %q, got:\n%s", want, out)
	}
}

func TestProgressViewUpdatesAndIgnoresMainKeys(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	dir := t.TempDir()
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: filepath.Join(dir, "out.mp4")}
	m := New(s, WithRunner(runningFake(release)))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	if m.run.phase != runRunning {
		t.Fatalf("run.phase = %v, want runRunning", m.run.phase)
	}
	if m.run.progress.Percent != 50 {
		t.Fatalf("run.progress.Percent = %v, want 50", m.run.progress.Percent)
	}
	out := viewText(m)
	if !strings.Contains(out, "50%") {
		t.Errorf("progress view missing 50%%, got:\n%s", out)
	}

	// Main-screen keys are ignored while running.
	before := m.focus
	mm, _ = m.Update(key("tab"))
	m = mm.(Model)
	if m.focus != before {
		t.Error("tab changed focus while a run was in progress")
	}
}

func TestRunCompletionShowsResultAnyKeyReturns(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: outputPath}
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		return runner.Result{Output: job.Output, Size: 200_000_000}, nil
	}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)
	wantLen := m.pipeline.Len()

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	if m.run.phase != runDone {
		t.Fatalf("run.phase = %v, want runDone", m.run.phase)
	}
	out := viewText(m)
	if !strings.Contains(out, "Wrote "+outputPath) {
		t.Errorf("missing 'Wrote' line, got:\n%s", out)
	}
	if !strings.Contains(out, "276.1 MB") || !strings.Contains(out, "200.0 MB") {
		t.Errorf("missing before/after sizes, got:\n%s", out)
	}

	mm, _ = m.Update(key("z"))
	m = mm.(Model)
	if m.run.phase != runNone {
		t.Error("a key on the result view did not return to the main screen")
	}
	if m.pipeline.Len() != wantLen {
		t.Errorf("pipeline.Len() after the run = %d, want %d (unchanged)", m.pipeline.Len(), wantLen)
	}
}

func TestRunExitErrorShowsTailAnyKeyReturns(t *testing.T) {
	dir := t.TempDir()
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: filepath.Join(dir, "out.mp4")}
	exitErr := &runner.ExitError{Code: 1, Tail: []string{"Unknown encoder", "ffmpeg exited"}}
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		return runner.Result{}, exitErr
	}
	m := New(s, WithRunner(fake))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	if m.run.phase != runError {
		t.Fatalf("run.phase = %v, want runError", m.run.phase)
	}
	out := viewText(m)
	if !strings.Contains(out, "Unknown encoder") || !strings.Contains(out, "ffmpeg exited") {
		t.Errorf("missing exit error tail, got:\n%s", out)
	}

	mm, _ = m.Update(key("z"))
	m = mm.(Model)
	if m.run.phase != runNone {
		t.Error("a key on the error view did not return to the main screen")
	}
}

func TestEscWhileRunningPromptsCancel(t *testing.T) {
	release := make(chan struct{})
	dir := t.TempDir()
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: filepath.Join(dir, "out.mp4")}
	m := New(s, WithRunner(runningFake(release)))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	msg := cmd()
	// The listener command re-arms itself on every progress message; that
	// re-armed command (not a fresh one from later keypresses) is what
	// eventually receives the run's final message, exactly as a real
	// Bubble Tea program keeps re-invoking whatever Cmd Update last
	// returned for this stream.
	mm, cmd = m.Update(msg)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("expected a re-armed wait-for-message command after a progress update")
	}

	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if !m.run.pendingCancel {
		t.Fatal("esc did not set pendingCancel")
	}
	out := viewText(m)
	if !strings.Contains(out, "Cancel encoding? (y/n)") {
		t.Errorf("missing cancel prompt, got:\n%s", out)
	}

	// n resumes the progress view.
	mm, _ = m.Update(key("n"))
	m = mm.(Model)
	if m.run.pendingCancel {
		t.Error("n did not clear pendingCancel")
	}
	if m.run.phase != runRunning {
		t.Errorf("run.phase after n = %v, want runRunning", m.run.phase)
	}

	// esc, y: cancels the run's context; the fake observes ctx.Done and
	// returns ErrCanceled, which surfaces as a canceled result on the
	// still-armed listener command.
	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	mm, _ = m.Update(key("y"))
	m = mm.(Model)
	msg = cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	if m.run.phase != runDone || !m.run.canceled {
		t.Fatalf("run = %+v, want a canceled runDone", m.run)
	}
	out = viewText(m)
	if !strings.Contains(out, "Canceled") {
		t.Errorf("missing Canceled status, got:\n%s", out)
	}
	close(release)
}

func TestQuitWhileRunningPromptsThenCancelsAndQuits(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	dir := t.TempDir()
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo(), Output: filepath.Join(dir, "out.mp4")}
	m := New(s, WithRunner(runningFake(release)))
	m = resized(m, 100, 30)

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	mm, _ = m.Update(key("q"))
	m = mm.(Model)
	if !m.run.pendingQuit {
		t.Fatal("q did not set pendingQuit")
	}
	out := viewText(m)
	if !strings.Contains(out, "Quit and cancel encoding? (y/n)") {
		t.Errorf("missing quit prompt, got:\n%s", out)
	}

	mm, quitCmd := m.Update(key("y"))
	m = mm.(Model)
	if quitCmd == nil {
		t.Fatal("y did not issue a quit command")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg")
	}
}

func TestDefaultRunnerIsRunnerRun(t *testing.T) {
	m := New(testSession(pipeline.New()))
	got := runtime.FuncForPC(reflect.ValueOf(m.runFn).Pointer()).Name()
	want := runtime.FuncForPC(reflect.ValueOf(runner.Run).Pointer()).Name()
	if got != want {
		t.Errorf("default runFn = %s, want %s", got, want)
	}
}
