package cli

import (
	"errors"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
)

func kindsOf(steps []pipeline.Step) []pipeline.Kind {
	ks := make([]pipeline.Kind, len(steps))
	for i, s := range steps {
		ks[i] = s.Kind()
	}
	return ks
}

func kindsEqual(a, b []pipeline.Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseStepOrder(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--speed", "2", "--width", "1280"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Input != "in.mov" {
		t.Errorf("Input = %q, want in.mov", cfg.Input)
	}
	got := kindsOf(cfg.Pipeline.Steps())
	want := []pipeline.Kind{pipeline.KindSpeed, pipeline.KindResolution}
	if !kindsEqual(got, want) {
		t.Errorf("step kinds = %v, want %v", got, want)
	}
	res := mustFind(t, cfg.Pipeline, pipeline.KindResolution).(pipeline.Resolution)
	if res.Width != 1280 || res.Height != 0 {
		t.Errorf("Resolution = %+v, want Width 1280, Height 0", res)
	}
	if !cfg.HasSteps {
		t.Error("HasSteps = false, want true")
	}
}

func mustFind(t *testing.T, p pipeline.Pipeline, k pipeline.Kind) pipeline.Step {
	t.Helper()
	s, ok := p.Find(k)
	if !ok {
		t.Fatalf("Find(%v) not found", k)
	}
	return s
}

func TestParseWidthHeightMergeAtFirstPosition(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--width", "1920", "--speed", "2", "--height", "1080"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := kindsOf(cfg.Pipeline.Steps())
	want := []pipeline.Kind{pipeline.KindResolution, pipeline.KindSpeed}
	if !kindsEqual(got, want) {
		t.Errorf("step kinds = %v, want %v (resolution at first position)", got, want)
	}
	res := mustFind(t, cfg.Pipeline, pipeline.KindResolution).(pipeline.Resolution)
	if res.Width != 1920 || res.Height != 1080 {
		t.Errorf("Resolution = %+v, want 1920x1080", res)
	}
}

func TestParseTrimStartEndMerge(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--trim-start", "0:05", "--trim-end", "20"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := kindsOf(cfg.Pipeline.Steps())
	want := []pipeline.Kind{pipeline.KindTrim}
	if !kindsEqual(got, want) {
		t.Errorf("step kinds = %v, want %v", got, want)
	}
	trim := mustFind(t, cfg.Pipeline, pipeline.KindTrim).(pipeline.Trim)
	if trim.Start != 5 || trim.End != 20 {
		t.Errorf("Trim = %+v, want {5,20}", trim)
	}
}

func TestParseTrimZeroStartWithEndInEitherOrder(t *testing.T) {
	orders := [][]string{
		{"in.mov", "--trim-start", "0", "--trim-end", "20"},
		{"in.mov", "--trim-end", "20", "--trim-start", "0"},
	}
	for _, args := range orders {
		cfg, err := Parse(args)
		if err != nil {
			t.Errorf("Parse(%q): %v", args, err)
			continue
		}
		trim := mustFind(t, cfg.Pipeline, pipeline.KindTrim).(pipeline.Trim)
		if trim.Start != 0 || trim.End != 20 {
			t.Errorf("Parse(%q) Trim = %+v, want {0,20}", args, trim)
		}
	}
}

func TestParseTrimKeepsPositionOfFirstTrimFlag(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--trim-start", "0", "--speed", "2", "--trim-end", "20"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := kindsOf(cfg.Pipeline.Steps())
	want := []pipeline.Kind{pipeline.KindTrim, pipeline.KindSpeed}
	if !kindsEqual(got, want) {
		t.Errorf("step kinds = %v, want %v (trim at first position)", got, want)
	}
}

func TestParseTrimStartZeroAloneErrors(t *testing.T) {
	if _, err := Parse([]string{"in.mov", "--trim-start", "0"}); !errors.Is(err, ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestParseTrimEndBeforeStartErrors(t *testing.T) {
	if _, err := Parse([]string{"in.mov", "--trim-end", "5", "--trim-start", "10"}); !errors.Is(err, ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestParseAllStepFlags(t *testing.T) {
	cfg, err := Parse([]string{
		"in.mov",
		"--scale", "50%",
		"--speed", "1.5",
		"--trim-start", "0:05",
		"--trim-end", "20",
		"--fps", "30",
		"--encoder", "h265",
		"--crf", "20",
		"--audio", "aac:96k",
		"--container", "webm",
		"--args", "-map_metadata -1",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.HasSteps {
		t.Error("HasSteps = false, want true")
	}
	res := mustFind(t, cfg.Pipeline, pipeline.KindResolution).(pipeline.Resolution)
	if res.Percent != 50 {
		t.Errorf("Resolution.Percent = %v, want 50", res.Percent)
	}
	sp := mustFind(t, cfg.Pipeline, pipeline.KindSpeed).(pipeline.Speed)
	if sp.Factor != 1.5 {
		t.Errorf("Speed.Factor = %v, want 1.5", sp.Factor)
	}
	fr := mustFind(t, cfg.Pipeline, pipeline.KindFrameRate).(pipeline.FrameRate)
	if fr.FPS != 30 {
		t.Errorf("FrameRate.FPS = %v, want 30", fr.FPS)
	}
	enc := mustFind(t, cfg.Pipeline, pipeline.KindEncoder).(pipeline.Encoder)
	if enc.Codec != pipeline.CodecH265 {
		t.Errorf("Encoder.Codec = %v, want CodecH265", enc.Codec)
	}
	q := mustFind(t, cfg.Pipeline, pipeline.KindQuality).(pipeline.Quality)
	if q.CRF != 20 {
		t.Errorf("Quality.CRF = %v, want 20", q.CRF)
	}
	aud := mustFind(t, cfg.Pipeline, pipeline.KindAudio).(pipeline.Audio)
	if aud.Mode != pipeline.AudioAAC || aud.BitrateK != 96 {
		t.Errorf("Audio = %+v, want AAC 96k", aud)
	}
	cont := mustFind(t, cfg.Pipeline, pipeline.KindContainer).(pipeline.Container)
	if cont.Format != pipeline.FormatWebM {
		t.Errorf("Container.Format = %v, want webm", cont.Format)
	}
	raw := mustFind(t, cfg.Pipeline, pipeline.KindRawArgs).(pipeline.RawArgs)
	if len(raw.Args) != 2 || raw.Args[0] != "-map_metadata" || raw.Args[1] != "-1" {
		t.Errorf("RawArgs.Args = %v, want [-map_metadata -1]", raw.Args)
	}
}

func TestParseTargetSize(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--target-size", "20MB"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	q := mustFind(t, cfg.Pipeline, pipeline.KindQuality).(pipeline.Quality)
	if q.TargetBytes != 20_000_000 {
		t.Errorf("Quality.TargetBytes = %v, want 20000000", q.TargetBytes)
	}
}

func TestParseEqualsForm(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--speed=2"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sp := mustFind(t, cfg.Pipeline, pipeline.KindSpeed).(pipeline.Speed)
	if sp.Factor != 2 {
		t.Errorf("Speed.Factor = %v, want 2", sp.Factor)
	}
}

func TestParseValueFlagConsumesDashPrefixedValue(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--trim-start", "-5"})
	// units.ParseTime rejects a negative value, so this should be a usage
	// error from the parser reaching ParseTime("-5"), not from treating
	// "-5" as a separate flag/positional.
	if err == nil {
		t.Fatalf("Parse: expected error, got Config %+v", cfg)
	}
	if !errors.Is(err, ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestParseDoubleDashEndsFlags(t *testing.T) {
	cfg, err := Parse([]string{"--speed", "2", "--", "in.mov"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Input != "in.mov" {
		t.Errorf("Input = %q, want in.mov", cfg.Input)
	}
}

func TestParseOutputFlag(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "-o", "out.mp4"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Output != "out.mp4" {
		t.Errorf("Output = %q, want out.mp4", cfg.Output)
	}

	cfg2, err := Parse([]string{"in.mov", "-o=out2.mp4"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg2.Output != "out2.mp4" {
		t.Errorf("Output = %q, want out2.mp4", cfg2.Output)
	}
}

func TestParseBoolFlags(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--tui", "--force"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.TUI || !cfg.Force {
		t.Errorf("cfg = %+v, want TUI and Force true", cfg)
	}

	cfg2, err := Parse([]string{"in.mov", "--in-place"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg2.InPlace {
		t.Error("InPlace = false, want true")
	}

	cfg3, err := Parse([]string{"in.mov", "--dry-run"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg3.DryRun {
		t.Error("DryRun = false, want true")
	}

	cfg4, err := Parse([]string{"-h"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg4.ShowHelp {
		t.Error("ShowHelp = false, want true")
	}

	cfg5, err := Parse([]string{"--help"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg5.ShowHelp {
		t.Error("ShowHelp = false, want true")
	}

	cfg6, err := Parse([]string{"--version"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg6.ShowVersion {
		t.Error("ShowVersion = false, want true")
	}
}

func TestParseStretchFlag(t *testing.T) {
	cfg, err := Parse([]string{"in.mov", "--width", "1920", "--height", "1080", "--stretch"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res := mustFind(t, cfg.Pipeline, pipeline.KindResolution).(pipeline.Resolution)
	if !res.Exact {
		t.Error("Resolution.Exact = false, want true")
	}
}

func TestParseStretchWithoutBothSidesError(t *testing.T) {
	_, err := Parse([]string{"in.mov", "--width", "1920", "--stretch"})
	if !errors.Is(err, ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

// --- usage errors ---

func TestParseUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"unknown flag":         {"in.mov", "--bogus"},
		"missing value":        {"in.mov", "--speed"},
		"same flag twice":      {"in.mov", "--speed", "2", "--speed", "3"},
		"scale with width":     {"in.mov", "--scale", "50%", "--width", "100"},
		"width with scale":     {"in.mov", "--width", "100", "--scale", "50%"},
		"crf with target-size": {"in.mov", "--crf", "20", "--target-size", "20MB"},
		"target-size with crf": {"in.mov", "--target-size", "20MB", "--crf", "20"},
		"o with in-place":      {"in.mov", "-o", "out.mp4", "--in-place"},
		"tui with dry-run":     {"in.mov", "--tui", "--dry-run"},
		"two positionals":      {"in.mov", "extra.mov"},
		"invalid speed":        {"in.mov", "--speed", "bogus"},
	}
	for name, args := range cases {
		if _, err := Parse(args); !errors.Is(err, ErrUsage) {
			t.Errorf("%s: err = %v, want ErrUsage", name, err)
		}
	}
}

func TestParseInPlaceSameContainerOK(t *testing.T) {
	_, err := Parse([]string{"in.mkv", "--in-place", "--container", "mkv"})
	if err != nil {
		t.Errorf("Parse: unexpected error %v", err)
	}
}

// TestParseInPlaceContainerMismatchIsAppsJob documents that Parse itself no
// longer rejects an --in-place container mismatch: it cannot tell a file
// input (whose own extension matters) from a directory input (whose name
// just happens to have no extension), so that check lives where the answer
// is known, in the app package.
func TestParseInPlaceContainerMismatchIsAppsJob(t *testing.T) {
	if _, err := Parse([]string{"in.mov", "--in-place", "--container", "mkv"}); err != nil {
		t.Errorf("Parse: unexpected error %v", err)
	}
}

func TestParseInPlaceOnDirectoryLikeInputDoesNotError(t *testing.T) {
	if _, err := Parse([]string{"/Users/me/Desktop", "--in-place", "--container", "mp4"}); err != nil {
		t.Errorf("Parse: unexpected error %v", err)
	}
}

func TestParseNoStepsHasStepsFalse(t *testing.T) {
	cfg, err := Parse([]string{"in.mov"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.HasSteps {
		t.Error("HasSteps = true, want false")
	}
}

// --- optional positional ---

func TestParseNoInputAllowed(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Input != "" {
		t.Errorf("Input = %q, want empty", cfg.Input)
	}
}

func TestParseStepFlagsWithoutInputAllowed(t *testing.T) {
	cfg, err := Parse([]string{"--speed", "2"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.HasSteps {
		t.Error("HasSteps = false, want true")
	}
}

func TestUsageListsFlags(t *testing.T) {
	for _, flag := range []string{
		"--width", "--height", "--scale", "--speed", "--trim-start", "--trim-end",
		"--fps", "--encoder", "--crf", "--target-size", "--audio", "--container",
		"--args", "--stretch", "-o", "--in-place", "--force", "--tui", "--dry-run",
		"--version", "-h", "--help",
	} {
		if !contains(Usage, flag) {
			t.Errorf("Usage missing %q", flag)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
