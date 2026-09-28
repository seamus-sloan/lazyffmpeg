package pipeline

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestKindIsFilter(t *testing.T) {
	filters := []Kind{KindResolution, KindSpeed, KindTrim, KindFrameRate}
	outputs := []Kind{KindEncoder, KindQuality, KindAudio, KindContainer, KindRawArgs}
	for _, k := range filters {
		if !k.IsFilter() {
			t.Errorf("%v.IsFilter() = false, want true", k)
		}
	}
	for _, k := range outputs {
		if k.IsFilter() {
			t.Errorf("%v.IsFilter() = true, want false", k)
		}
	}
}

func TestKindLabel(t *testing.T) {
	cases := map[Kind]string{
		KindResolution: "Resolution",
		KindSpeed:      "Speed",
		KindTrim:       "Trim",
		KindFrameRate:  "Frame rate",
		KindEncoder:    "Encoder",
		KindQuality:    "Quality",
		KindAudio:      "Audio",
		KindContainer:  "Container",
		KindRawArgs:    "Raw args",
	}
	for k, want := range cases {
		if got := k.Label(); got != want {
			t.Errorf("%v.Label() = %q, want %q", k, got, want)
		}
	}
}

// --- Resolution ---

func TestResolutionValidate(t *testing.T) {
	valid := []Resolution{
		{Width: 1920, Height: 1080},
		{Width: 1280},
		{Height: 720},
		{Percent: 50},
		{Width: 1920, Height: 1080, Exact: true},
	}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", r, err)
		}
	}
	invalid := []Resolution{
		{},                                       // neither percent nor sides
		{Width: 1920, Height: 1080, Percent: 50}, // both
		{Width: 1},                               // below min
		{Width: 20000},                           // above max
		{Percent: 401},                           // above max
		{Percent: 0},                             // zero
		{Width: 1920, Exact: true},               // exact needs both sides
		{Percent: math.NaN()},                    // NaN percent
		{Percent: math.Inf(1)},                   // +Inf percent
		{Percent: math.Inf(-1)},                  // -Inf percent
		{Width: 1920, Height: 1080, Percent: math.NaN()}, // NaN percent alongside valid sides
	}
	for _, r := range invalid {
		if err := r.Validate(); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidStep", r, err)
		}
	}
}

func TestResolutionVideoFilterOddSingleSideRoundsDownToEven(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"853x", "scale=852:-2"},
		{"x481", "scale=-2:480"},
		{"161x", "scale=160:-2"},
	}
	for _, c := range cases {
		r, err := ParseResolution(c.in)
		if err != nil {
			t.Fatalf("ParseResolution(%q): %v", c.in, err)
		}
		if got := r.VideoFilter(); got != c.want {
			t.Errorf("VideoFilter(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolutionVideoFilter(t *testing.T) {
	cases := []struct {
		r    Resolution
		want string
	}{
		{Resolution{Width: 1920, Height: 1080, Exact: true}, "scale=1920:1080"},
		{Resolution{Width: 1921, Height: 1081, Exact: true}, "scale=1920:1080"},
		{Resolution{Width: 1280}, "scale=1280:-2"},
		{Resolution{Height: 720}, "scale=-2:720"},
		{Resolution{Percent: 50}, "scale=trunc(iw*0.5/2)*2:-2"},
		{Resolution{Width: 1920, Height: 1080}, "scale=1920:1080:force_original_aspect_ratio=decrease:force_divisible_by=2"},
		{Resolution{Width: 1280, Height: 720}, "scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2"},
	}
	for _, c := range cases {
		if got := c.r.VideoFilter(); got != c.want {
			t.Errorf("VideoFilter(%+v) = %q, want %q", c.r, got, c.want)
		}
		if got := c.r.AudioFilter(); got != "" {
			t.Errorf("AudioFilter(%+v) = %q, want empty", c.r, got)
		}
	}
}

func TestResolutionNormalize(t *testing.T) {
	cases := []struct {
		r, want Resolution
	}{
		{Resolution{Width: 853}, Resolution{Width: 852}},
		{Resolution{Height: 481}, Resolution{Height: 480}},
		{Resolution{Width: 3}, Resolution{Width: 2}},
		{Resolution{Width: 1280}, Resolution{Width: 1280}},
		{Resolution{Width: 1921, Height: 1081, Exact: true}, Resolution{Width: 1920, Height: 1080, Exact: true}},
		// A fit box is a bound, not the output size: its sides stay as given.
		{Resolution{Width: 1921, Height: 1081}, Resolution{Width: 1921, Height: 1081}},
		{Resolution{Percent: 33}, Resolution{Percent: 33}},
	}
	for _, c := range cases {
		if got := c.r.Normalize(); got != c.want {
			t.Errorf("Normalize(%+v) = %+v, want %+v", c.r, got, c.want)
		}
	}
}

func TestResolutionSummary(t *testing.T) {
	cases := []struct {
		r    Resolution
		want string
	}{
		{Resolution{Width: 1920, Height: 1080, Exact: true}, "1920×1080 (stretch)"},
		{Resolution{Width: 1920, Height: 1080}, "fit 1920×1080"},
		{Resolution{Width: 1280}, "1280×auto"},
		{Resolution{Height: 720}, "auto×720"},
		{Resolution{Percent: 50}, "50%"},
		{Resolution{Width: 853}, "852×auto"},
		{Resolution{Height: 481}, "auto×480"},
		{Resolution{Width: 1921, Height: 1081, Exact: true}, "1920×1080 (stretch)"},
		{Resolution{Width: 1921, Height: 1081}, "fit 1921×1081"},
	}
	for _, c := range cases {
		if got := c.r.Summary(); got != c.want {
			t.Errorf("Summary(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

// --- Speed ---

func TestSpeedValidate(t *testing.T) {
	if err := (Speed{Factor: 0.01}).Validate(); err != nil {
		t.Errorf("Validate(0.01) = %v, want nil", err)
	}
	if err := (Speed{Factor: 100}).Validate(); err != nil {
		t.Errorf("Validate(100) = %v, want nil", err)
	}
	if err := (Speed{Factor: 0.001}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(0.001) = %v, want ErrInvalidStep", err)
	}
	if err := (Speed{Factor: 101}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(101) = %v, want ErrInvalidStep", err)
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := (Speed{Factor: f}).Validate(); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("Validate(%v) = %v, want ErrInvalidStep", f, err)
		}
	}
}

func TestSpeedFilters(t *testing.T) {
	cases := []struct {
		factor    float64
		wantVideo string
		wantAudio string
	}{
		{2, "setpts=PTS/2", "atempo=2.0"},
		{4, "setpts=PTS/4", "atempo=2.0,atempo=2.0"},
		{3, "setpts=PTS/3", "atempo=2.0,atempo=1.5"},
		{5, "setpts=PTS/5", "atempo=2.0,atempo=2.0,atempo=1.25"},
		{1.5, "setpts=PTS/1.5", "atempo=1.5"},
		{0.25, "setpts=PTS/0.25", "atempo=0.5,atempo=0.5"},
		{0.3, "setpts=PTS/0.3", "atempo=0.5,atempo=0.6"},
		{1, "", ""},
	}
	for _, c := range cases {
		s := Speed{Factor: c.factor}
		if got := s.VideoFilter(); got != c.wantVideo {
			t.Errorf("Speed{%v}.VideoFilter() = %q, want %q", c.factor, got, c.wantVideo)
		}
		if got := s.AudioFilter(); got != c.wantAudio {
			t.Errorf("Speed{%v}.AudioFilter() = %q, want %q", c.factor, got, c.wantAudio)
		}
	}
}

func TestSpeedSummary(t *testing.T) {
	if got := (Speed{Factor: 2}).Summary(); got != "2x" {
		t.Errorf("Summary(2) = %q, want 2x", got)
	}
	if got := (Speed{Factor: 1.5}).Summary(); got != "1.5x" {
		t.Errorf("Summary(1.5) = %q, want 1.5x", got)
	}
}

// --- Trim ---

func TestTrimValidate(t *testing.T) {
	valid := []Trim{{Start: 5, End: 20}, {Start: 5}, {Start: 0, End: 20}}
	for _, tr := range valid {
		if err := tr.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", tr, err)
		}
	}
	invalid := []Trim{
		{Start: -1}, {}, {Start: 20, End: 5}, {Start: 20, End: 20},
		{Start: math.NaN(), End: 20}, {Start: 5, End: math.NaN()},
		{Start: math.Inf(1)}, {Start: 5, End: math.Inf(1)},
	}
	for _, tr := range invalid {
		if err := tr.Validate(); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidStep", tr, err)
		}
	}
}

func TestTrimFilters(t *testing.T) {
	cases := []struct {
		trim      Trim
		wantVideo string
		wantAudio string
	}{
		{Trim{5, 20}, "trim=start=5:end=20,setpts=PTS-STARTPTS", "atrim=start=5:end=20,asetpts=PTS-STARTPTS"},
		{Trim{5, 0}, "trim=start=5,setpts=PTS-STARTPTS", "atrim=start=5,asetpts=PTS-STARTPTS"},
		{Trim{0, 20}, "trim=start=0:end=20,setpts=PTS-STARTPTS", "atrim=start=0:end=20,asetpts=PTS-STARTPTS"},
	}
	for _, c := range cases {
		if got := c.trim.VideoFilter(); got != c.wantVideo {
			t.Errorf("VideoFilter(%+v) = %q, want %q", c.trim, got, c.wantVideo)
		}
		if got := c.trim.AudioFilter(); got != c.wantAudio {
			t.Errorf("AudioFilter(%+v) = %q, want %q", c.trim, got, c.wantAudio)
		}
	}
}

func TestTrimSummary(t *testing.T) {
	cases := []struct {
		trim Trim
		want string
	}{
		{Trim{5, 20}, "00:05 → 00:20"},
		{Trim{5, 0}, "00:05 → end"},
		{Trim{0, 20}, "start → 00:20"},
	}
	for _, c := range cases {
		if got := c.trim.Summary(); got != c.want {
			t.Errorf("Summary(%+v) = %q, want %q", c.trim, got, c.want)
		}
	}
}

// --- FrameRate ---

func TestFrameRateValidate(t *testing.T) {
	if err := (FrameRate{FPS: 30}).Validate(); err != nil {
		t.Errorf("Validate(30) = %v, want nil", err)
	}
	if err := (FrameRate{FPS: 0}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(0) = %v, want ErrInvalidStep", err)
	}
	if err := (FrameRate{FPS: 241}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(241) = %v, want ErrInvalidStep", err)
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := (FrameRate{FPS: f}).Validate(); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("Validate(%v) = %v, want ErrInvalidStep", f, err)
		}
	}
}

func TestFrameRateFilters(t *testing.T) {
	if got := (FrameRate{FPS: 30}).VideoFilter(); got != "fps=30" {
		t.Errorf("VideoFilter(30) = %q, want fps=30", got)
	}
	if got := (FrameRate{FPS: 29.97}).VideoFilter(); got != "fps=29.97" {
		t.Errorf("VideoFilter(29.97) = %q, want fps=29.97", got)
	}
	if got := (FrameRate{FPS: 30}).AudioFilter(); got != "" {
		t.Errorf("AudioFilter(30) = %q, want empty", got)
	}
}

func TestFrameRateSummary(t *testing.T) {
	if got := (FrameRate{FPS: 30}).Summary(); got != "30 fps" {
		t.Errorf("Summary(30) = %q, want '30 fps'", got)
	}
}

// --- Encoder ---

func TestEncoderValidate(t *testing.T) {
	known := []Codec{CodecH264, CodecH265, CodecAV1, CodecVP9, CodecH264HW, CodecH265HW, CodecCopy}
	for _, c := range known {
		if err := (Encoder{Codec: c}).Validate(); err != nil {
			t.Errorf("Validate(%v) = %v, want nil", c, err)
		}
	}
	if err := (Encoder{Codec: "bogus"}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(bogus) = %v, want ErrInvalidStep", err)
	}
}

func TestEncoderSummary(t *testing.T) {
	cases := map[Codec]string{
		CodecH264:   "H.264 (libx264)",
		CodecH265:   "H.265 (libx265)",
		CodecAV1:    "AV1 (libsvtav1)",
		CodecVP9:    "VP9 (libvpx-vp9)",
		CodecH264HW: "H.264 hardware (h264_videotoolbox)",
		CodecH265HW: "H.265 hardware (hevc_videotoolbox)",
		CodecCopy:   "Copy (no re-encode)",
	}
	for c, want := range cases {
		if got := (Encoder{Codec: c}).Summary(); got != want {
			t.Errorf("Summary(%v) = %q, want %q", c, got, want)
		}
	}
}

// --- Quality ---

func TestQualityValidate(t *testing.T) {
	if err := (Quality{CRF: 23}).Validate(); err != nil {
		t.Errorf("Validate(CRF 23) = %v, want nil", err)
	}
	if err := (Quality{TargetBytes: 20_000_000}).Validate(); err != nil {
		t.Errorf("Validate(target) = %v, want nil", err)
	}
	if err := (Quality{CRF: 64}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(CRF 64) = %v, want ErrInvalidStep", err)
	}
}

func TestQualitySummary(t *testing.T) {
	if got := (Quality{CRF: 23}).Summary(); got != "CRF 23" {
		t.Errorf("Summary(CRF 23) = %q, want 'CRF 23'", got)
	}
	if got := (Quality{TargetBytes: 20_000_000}).Summary(); got != "target 20.0 MB" {
		t.Errorf("Summary(target) = %q, want 'target 20.0 MB'", got)
	}
}

// --- Audio ---

func TestAudioValidate(t *testing.T) {
	if err := (Audio{Mode: AudioKeep}).Validate(); err != nil {
		t.Errorf("Validate(keep) = %v, want nil", err)
	}
	if err := (Audio{Mode: AudioRemove}).Validate(); err != nil {
		t.Errorf("Validate(remove) = %v, want nil", err)
	}
	if err := (Audio{Mode: AudioAAC, BitrateK: 128}).Validate(); err != nil {
		t.Errorf("Validate(aac 128) = %v, want nil", err)
	}
	if err := (Audio{Mode: AudioAAC, BitrateK: 10}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(aac 10) = %v, want ErrInvalidStep", err)
	}
	if err := (Audio{Mode: AudioKeep, BitrateK: 128}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(keep w/ bitrate) = %v, want ErrInvalidStep", err)
	}
}

func TestAudioSummary(t *testing.T) {
	if got := (Audio{Mode: AudioKeep}).Summary(); got != "keep" {
		t.Errorf("Summary(keep) = %q, want keep", got)
	}
	if got := (Audio{Mode: AudioRemove}).Summary(); got != "remove" {
		t.Errorf("Summary(remove) = %q, want remove", got)
	}
	if got := (Audio{Mode: AudioAAC, BitrateK: 128}).Summary(); got != "AAC 128k" {
		t.Errorf("Summary(aac 128) = %q, want 'AAC 128k'", got)
	}
}

// --- Container ---

func TestContainerValidate(t *testing.T) {
	for _, f := range []Format{FormatMP4, FormatMOV, FormatMKV, FormatWebM} {
		if err := (Container{Format: f}).Validate(); err != nil {
			t.Errorf("Validate(%v) = %v, want nil", f, err)
		}
	}
	if err := (Container{Format: "avi"}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(avi) = %v, want ErrInvalidStep", err)
	}
}

func TestContainerSummary(t *testing.T) {
	if got := (Container{Format: FormatMP4}).Summary(); got != "mp4" {
		t.Errorf("Summary(mp4) = %q, want mp4", got)
	}
}

// --- RawArgs ---

func TestRawArgsValidate(t *testing.T) {
	if err := (RawArgs{Text: "-an", Args: []string{"-an"}}).Validate(); err != nil {
		t.Errorf("Validate(non-empty) = %v, want nil", err)
	}
	if err := (RawArgs{}).Validate(); !errors.Is(err, ErrInvalidStep) {
		t.Errorf("Validate(empty) = %v, want ErrInvalidStep", err)
	}
}

func TestRawArgsSummary(t *testing.T) {
	if got := (RawArgs{Text: "-map_metadata -1"}).Summary(); got != "-map_metadata -1" {
		t.Errorf("Summary = %q, want verbatim text", got)
	}
}

// --- Filename ---

func TestKindFilenameIsAnOutputSettingLabelledFileName(t *testing.T) {
	if KindFilename.IsFilter() {
		t.Error("KindFilename.IsFilter() = true, want false")
	}
	if got := KindFilename.Label(); got != "File name" {
		t.Errorf("KindFilename.Label() = %q, want %q", got, "File name")
	}
	if got := (Filename{Name: "demo"}).Kind(); got != KindFilename {
		t.Errorf("Filename.Kind() = %v, want KindFilename", got)
	}
}

func TestFilenameValidate(t *testing.T) {
	valid := []string{"demo", "demo.mp4", "my.clip", "Screen Recording at 1.02 PM (edited).mov", "Clip — été.mov", "a.", strings.Repeat("n", 255)}
	for _, name := range valid {
		if err := (Filename{Name: name}).Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", ".", "..", "a/b", "/abs.mp4", "sub/", `a\b`, strings.Repeat("n", 256),
		// Hidden files.
		".mov", ".hidden", "...", ". demo",
		// Control characters.
		"a\tb", "a\nb", "a\rb", "a\x00b", "\x1b[31mred.mp4", "a\x7fb", "end\x1f"}
	for _, name := range invalid {
		if err := (Filename{Name: name}).Validate(); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("Validate(%q) = %v, want ErrInvalidStep", name, err)
		}
	}
}

func TestFilenameSummaryIsTheNameAsGiven(t *testing.T) {
	if got := (Filename{Name: "My Clip.MOV"}).Summary(); got != "My Clip.MOV" {
		t.Errorf("Summary = %q, want %q", got, "My Clip.MOV")
	}
}
