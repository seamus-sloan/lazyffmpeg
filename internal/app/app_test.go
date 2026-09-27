package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/cli"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/testclip"
)

func newApp() (*app.App, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	a := &app.App{Stdout: &out, Stderr: &errOut}
	return a, &out, &errOut
}

func TestMainHelp(t *testing.T) {
	a, out, _ := newApp()
	code := a.Main(context.Background(), []string{"--help"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out.String() != cli.Usage {
		t.Errorf("stdout = %q, want cli.Usage", out.String())
	}
}

func TestMainVersionDefault(t *testing.T) {
	old := app.Version
	app.Version = "dev"
	defer func() { app.Version = old }()

	a, out, _ := newApp()
	code := a.Main(context.Background(), []string{"--version"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out.String() != "lazyff dev\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "lazyff dev\n")
	}
}

func TestMainVersionLdflags(t *testing.T) {
	old := app.Version
	app.Version = "v1.2.3"
	defer func() { app.Version = old }()

	a, out, _ := newApp()
	code := a.Main(context.Background(), []string{"--version"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out.String() != "lazyff v1.2.3\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "lazyff v1.2.3\n")
	}
}

func TestMainUsageError(t *testing.T) {
	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{"--bogus"})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	s := errOut.String()
	if !strings.HasPrefix(s, "lazyff: ") {
		t.Errorf("stderr = %q, want prefix 'lazyff: '", s)
	}
	if !strings.Contains(s, "Run 'lazyff --help' for usage.") {
		t.Errorf("stderr = %q, want usage hint", s)
	}
}

func TestMainNoSuchFile(t *testing.T) {
	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{"/no/such/file-lazyff-test-123"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "lazyff: /no/such/file-lazyff-test-123: no such file or directory\n"
	if errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
}

func TestMainDryRun(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, out, _ := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "--dry-run"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "ffmpeg") {
		t.Errorf("stdout = %q, want it to contain 'ffmpeg'", out.String())
	}
	wantOut := filepath.Join(filepath.Dir(in), strings.TrimSuffix(filepath.Base(in), ".mp4")+" (edited).mp4")
	if !strings.Contains(out.String(), wantOut) {
		t.Errorf("stdout = %q, want it to contain %q", out.String(), wantOut)
	}
	if _, err := os.Stat(wantOut); err == nil {
		t.Error("dry-run should not create a file")
	}
}

func TestMainHeadlessSuccess(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})

	a, out, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	wantOut := filepath.Join(filepath.Dir(in), strings.TrimSuffix(filepath.Base(in), ".mp4")+" (edited).mp4")
	if _, err := os.Stat(wantOut); err != nil {
		t.Errorf("output not created: %v", err)
	}
	if !strings.Contains(out.String(), wantOut) {
		t.Errorf("stdout = %q, want it to contain output path %q", out.String(), wantOut)
	}
	if !strings.Contains(out.String(), "→") {
		t.Errorf("stdout = %q, want a before → after size line", out.String())
	}
}

func TestMainOutputExistsNoForce(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "existing.mp4")
	if err := os.WriteFile(out, []byte("marker"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "-o", out})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "output exists") {
		t.Errorf("stderr = %q, want it to mention 'output exists'", errOut.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "marker" {
		t.Error("existing output was modified without --force")
	}
}

func TestMainOutputExistsWithForce(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "existing.mp4")
	if err := os.WriteFile(out, []byte("marker"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "-o", out, "--force"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) == "marker" {
		t.Error("output should have been overwritten with --force")
	}
}

func TestMainSameAsInput(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "-o", in})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "in-place") {
		t.Errorf("stderr = %q, want it to mention --in-place", errOut.String())
	}
}

func TestMainInPlaceUnsupportedExtensionErrors(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Name: "clip.gif", Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "--in-place"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "-o") || !strings.Contains(errOut.String(), "--container") {
		t.Errorf("stderr = %q, want it to suggest -o or --container", errOut.String())
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("input should be untouched: %v", err)
	}
}

func TestMainInPlaceContainerMismatchStillErrors(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--in-place", "--container", "mkv"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "container") {
		t.Errorf("stderr = %q, want it to mention the container mismatch", errOut.String())
	}
}

func TestMainInPlaceOnGifWithSpeedProducesMP4(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Name: "clip.gif", Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	wantOut := filepath.Join(filepath.Dir(in), "clip (edited).mp4")
	if _, err := os.Stat(wantOut); err != nil {
		t.Errorf("output not created at %q: %v", wantOut, err)
	}
}

func TestMainInPlace(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Name: "clip at 1.02 PM.mp4", Width: 320, Height: 240, Seconds: 2})
	dir := filepath.Dir(in)

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "--in-place"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("in-place output missing: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".lazyff-*"))
	if len(matches) != 0 {
		t.Errorf("leftover temp files: %v", matches)
	}
}

func TestMainValidationError(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--encoder", "copy", "--speed", "2"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(errOut.String(), "lazyff: ") {
		t.Errorf("stderr = %q, want prefix 'lazyff: '", errOut.String())
	}
}

func TestMainFFmpegFailure(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--args", "-vf not_a_real_filter_xyz"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "lazyff: ffmpeg failed") {
		t.Errorf("stderr = %q, want it to contain 'lazyff: ffmpeg failed'", errOut.String())
	}
}

func TestMainCanceled(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 5})

	a, _, _ := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	code := a.Main(ctx, []string{in, "--speed", "2"})
	if code != 130 {
		t.Errorf("exit code = %d, want 130", code)
	}
}

func TestMainNoStepsLaunchesTUI(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	var gotSession app.Session
	called := 0
	a, _, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		called++
		gotSession = s
		return nil
	}
	code := a.Main(context.Background(), []string{in})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if called != 1 {
		t.Fatalf("LaunchTUI called %d times, want 1", called)
	}
	if gotSession.Input != in {
		t.Errorf("Session.Input = %q, want %q", gotSession.Input, in)
	}
	if gotSession.Info.Video.Width != 320 {
		t.Errorf("Session.Info not probed: %+v", gotSession.Info)
	}
}

func TestMainTUIFlagLaunchesTUIEvenWithSteps(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	called := 0
	a, _, _ := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		called++
		return nil
	}
	code := a.Main(context.Background(), []string{in, "--speed", "2", "--tui"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if called != 1 {
		t.Errorf("LaunchTUI called %d times, want 1", called)
	}
}

func TestMainLaunchTUINil(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "interactive mode not available") {
		t.Errorf("stderr = %q, want it to mention interactive mode", errOut.String())
	}
}

func TestMainLaunchTUIError(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		return errors.New("boom")
	}
	code := a.Main(context.Background(), []string{in})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("stderr = %q, want it to contain the launch error", errOut.String())
	}
}

func TestMainNoArgsLaunchesTUIWithCWD(t *testing.T) {
	var gotSession app.Session
	a, _, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		gotSession = s
		return nil
	}
	code := a.Main(context.Background(), nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if gotSession.Dir != cwd {
		t.Errorf("Session.Dir = %q, want %q", gotSession.Dir, cwd)
	}
	if gotSession.Input != "" {
		t.Errorf("Session.Input = %q, want empty", gotSession.Input)
	}
}

func TestMainDirectoryArgLaunchesTUI(t *testing.T) {
	dir := t.TempDir()
	var gotSession app.Session
	a, _, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		gotSession = s
		return nil
	}
	code := a.Main(context.Background(), []string{dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if gotSession.Dir != dir {
		t.Errorf("Session.Dir = %q, want %q", gotSession.Dir, dir)
	}
	if gotSession.Input != "" {
		t.Errorf("Session.Input = %q, want empty", gotSession.Input)
	}
}

func TestMainDirectoryArgWithStepFlagsLaunchesTUI(t *testing.T) {
	dir := t.TempDir()
	called := 0
	var gotSession app.Session
	a, _, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		called++
		gotSession = s
		return nil
	}
	code := a.Main(context.Background(), []string{dir, "--speed", "2"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if called != 1 {
		t.Fatalf("LaunchTUI called %d times, want 1 (should not run headless without a file)", called)
	}
	if _, ok := gotSession.Pipeline.Find(pipeline.KindSpeed); !ok {
		t.Errorf("Session.Pipeline missing Speed step: %+v", gotSession.Pipeline.Steps())
	}
}

func TestMainDryRunWithDirectoryUsageError(t *testing.T) {
	dir := t.TempDir()
	a, _, _ := newApp()
	code := a.Main(context.Background(), []string{dir, "--dry-run"})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestMainDryRunNoFileUsageError(t *testing.T) {
	a, _, _ := newApp()
	code := a.Main(context.Background(), []string{"--dry-run"})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestSameAsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !app.SameAsInput(in, in) {
		t.Error("SameAsInput(in, in) = false, want true")
	}
	if !app.SameAsInput(in, filepath.Join(dir, ".", "clip.mp4")) {
		t.Error("SameAsInput did not resolve an unclean path to the same file")
	}
	if app.SameAsInput(in, filepath.Join(dir, "out.mp4")) {
		t.Error("SameAsInput(in, out) = true, want false")
	}
}
