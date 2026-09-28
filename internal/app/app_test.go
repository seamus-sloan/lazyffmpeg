package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/cli"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
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

func TestMainDryRunWithoutStepsPrintsInsteadOfLaunchingTUI(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, out, errOut := newApp()
	a.LaunchTUI = func(ctx context.Context, s app.Session) error {
		t.Error("--dry-run must not launch the TUI")
		return nil
	}
	code := a.Main(context.Background(), []string{in, "--dry-run"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "ffmpeg ") {
		t.Errorf("stdout = %q, want the ffmpeg command", out.String())
	}
}

func TestMainDryRunIgnoresAnExistingOutput(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	existing := filepath.Join(filepath.Dir(in), "existing.mp4")
	if err := os.WriteFile(existing, []byte("marker"), 0644); err != nil {
		t.Fatal(err)
	}

	a, out, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "-o", existing, "--dry-run"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), existing) {
		t.Errorf("stdout = %q, want it to contain %q", out.String(), existing)
	}
}

func TestMainDryRunStillRejectsUnsafeOutputs(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	gif := testclip.Make(t, testclip.Spec{Name: "clip.gif", Width: 320, Height: 240, Seconds: 1})

	cases := map[string][]string{
		"same as input":         {in, "--speed", "2", "-o", in, "--dry-run"},
		"in-place unsupported":  {gif, "--speed", "2", "--in-place", "--dry-run"},
		"in-place wrong format": {in, "--in-place", "--container", "mkv", "--dry-run"},
		"invalid pipeline":      {in, "--encoder", "copy", "--speed", "2", "--dry-run"},
	}
	for name, args := range cases {
		a, out, _ := newApp()
		if code := a.Main(context.Background(), args); code != 1 {
			t.Errorf("%s: exit code = %d, want 1", name, code)
		}
		if out.Len() != 0 {
			t.Errorf("%s: stdout = %q, want no command printed", name, out.String())
		}
	}
}

func TestMainHeadlessSuccess(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})

	inInfo, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run(in): %v", err)
	}

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

	outInfo, err := probe.Run(context.Background(), wantOut)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	wantDur := inInfo.Duration / 2
	if diff := outInfo.Duration - wantDur; diff > 0.15 || diff < -0.15 {
		t.Errorf("output duration = %v, want ~%v (halved)", outInfo.Duration, wantDur)
	}

	lastPct := -1
	sawEnd := false
	for _, line := range strings.Split(strings.TrimSpace(errOut.String()), "\n") {
		pctStr, ok := strings.CutPrefix(line, "progress: ")
		if !ok {
			continue
		}
		pctStr = strings.TrimSuffix(pctStr, "%")
		pct, err := strconv.Atoi(pctStr)
		if err != nil {
			t.Fatalf("progress line %q: %v", line, err)
		}
		if pct < lastPct {
			t.Errorf("progress went backwards: %d after %d", pct, lastPct)
		}
		lastPct = pct
		if pct == 100 {
			sawEnd = true
		}
	}
	if !sawEnd {
		t.Errorf("stderr never reached progress: 100%%, got:\n%s", errOut.String())
	}
}

func TestMainHeadlessOddWidthEncodesEvenWidth(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--width", "161"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	out := filepath.Join(filepath.Dir(in), "clip (edited).mp4")
	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	if outInfo.Video.Width != 160 || outInfo.Video.Height != 120 {
		t.Errorf("output dims = %dx%d, want 160x120", outInfo.Video.Width, outInfo.Video.Height)
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
	wantSuggestion := "-o " + filepath.Join(filepath.Dir(in), "clip.mp4")
	if !strings.Contains(errOut.String(), wantSuggestion) {
		t.Errorf("stderr = %q, want it to suggest %q", errOut.String(), wantSuggestion)
	}
	if strings.Contains(errOut.String(), "--container") {
		t.Errorf("stderr = %q, should not suggest --container (it cannot help in place)", errOut.String())
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("input should be untouched: %v", err)
	}
}

func TestMainM4VInputWithH265WritesMP4(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Name: "clip.m4v", Width: 320, Height: 240, Seconds: 1})

	a, out, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--encoder", "h265"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	wantOut := filepath.Join(filepath.Dir(in), "clip (edited).mp4")
	if _, err := os.Stat(wantOut); err != nil {
		t.Errorf("output not created at %q: %v (stdout=%q)", wantOut, err, out.String())
	}
}

func TestMainInPlaceM4VWithH265Errors(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Name: "clip.m4v", Width: 320, Height: 240, Seconds: 1})
	before, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--encoder", "h265", "--in-place"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !errors.Is(app.CheckInPlace(in, pipeline.New(pipeline.Encoder{Codec: pipeline.CodecH265})), app.ErrInPlaceUnsupportedExt) {
		t.Errorf("CheckInPlace(.m4v, h265) should be ErrInPlaceUnsupportedExt")
	}
	wantSuggestion := "-o " + filepath.Join(filepath.Dir(in), "clip.mp4")
	if !strings.Contains(errOut.String(), wantSuggestion) {
		t.Errorf("stderr = %q, want it to suggest %q", errOut.String(), wantSuggestion)
	}
	after, err := os.ReadFile(in)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("input should be untouched (err=%v)", err)
	}
}

func TestCheckInPlace(t *testing.T) {
	h264 := pipeline.New(pipeline.Encoder{Codec: pipeline.CodecH264})
	cases := []struct {
		name  string
		input string
		p     pipeline.Pipeline
		want  error
	}{
		{"mp4 default encoder", "/v/clip.mp4", pipeline.New(), nil},
		{"m4v default encoder", "/v/clip.m4v", pipeline.New(), nil},
		{"m4v h264", "/v/clip.m4v", h264, nil},
		{"m4v h264 hardware", "/v/clip.m4v", pipeline.New(pipeline.Encoder{Codec: pipeline.CodecH264HW}), nil},
		{"m4v copy", "/v/clip.m4v", pipeline.New(pipeline.Encoder{Codec: pipeline.CodecCopy}), nil},
		{"m4v h265", "/v/clip.m4v", pipeline.New(pipeline.Encoder{Codec: pipeline.CodecH265}), app.ErrInPlaceUnsupportedExt},
		{"m4v av1", "/v/clip.M4V", pipeline.New(pipeline.Encoder{Codec: pipeline.CodecAV1}), app.ErrInPlaceUnsupportedExt},
		{"gif", "/v/clip.gif", pipeline.New(), app.ErrInPlaceUnsupportedExt},
		{"mov with mkv container", "/v/clip.mov", pipeline.New(pipeline.Container{Format: pipeline.FormatMKV}), app.ErrInPlaceContainerMismatch},
	}
	for _, c := range cases {
		err := app.CheckInPlace(c.input, c.p)
		if c.want == nil && err != nil {
			t.Errorf("%s: CheckInPlace = %v, want nil", c.name, err)
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: CheckInPlace = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestSessionOutputPathPrecedence(t *testing.T) {
	named := pipeline.New(pipeline.Filename{Name: "demo"})
	cases := []struct {
		name string
		s    app.Session
		p    pipeline.Pipeline
		want string
	}{
		{"in-place wins over a file name", app.Session{Input: "/v/clip.mov", InPlace: true}, named, "/v/clip.mov"},
		{"file name, next to the input", app.Session{Input: "/v/clip.mov"}, named, "/v/demo.mov"},
		{"file name wins over -o, in -o's directory", app.Session{Input: "/v/clip.mov", Output: "/out/x.mp4"}, named, "/out/demo.mov"},
		{"-o without a file name", app.Session{Input: "/v/clip.mov", Output: "/out/x.mp4"}, pipeline.New(), "/out/x.mp4"},
		{"default", app.Session{Input: "/v/clip.mov"}, pipeline.New(), "/v/clip (edited).mov"},
	}
	for _, c := range cases {
		if got := c.s.OutputPath(c.p); got != c.want {
			t.Errorf("%s: OutputPath = %q, want %q", c.name, got, c.want)
		}
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

	inInfo, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run(in): %v", err)
	}

	a, _, errOut := newApp()
	code := a.Main(context.Background(), []string{in, "--speed", "2", "--in-place"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0, stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("in-place output missing: %v", err)
	}

	outInfo, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run(in-place result): %v", err)
	}
	wantDur := inInfo.Duration / 2
	if diff := outInfo.Duration - wantDur; diff > 0.15 || diff < -0.15 {
		t.Errorf("in-place output duration = %v, want ~%v (halved)", outInfo.Duration, wantDur)
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
