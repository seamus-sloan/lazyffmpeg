package pipeline

import (
	"errors"
	"testing"
)

func TestParseResolution(t *testing.T) {
	cases := []struct {
		in   string
		want Resolution
	}{
		{"1920x1080", Resolution{Width: 1920, Height: 1080}},
		{"1920×1080", Resolution{Width: 1920, Height: 1080}},
		{"1920X1080", Resolution{Width: 1920, Height: 1080}},
		{"1280x", Resolution{Width: 1280}},
		{"x720", Resolution{Height: 720}},
		{"50%", Resolution{Percent: 50}},
		{"1920x1080!", Resolution{Width: 1920, Height: 1080, Exact: true}},
	}
	for _, c := range cases {
		got, err := ParseResolution(c.in)
		if err != nil {
			t.Errorf("ParseResolution(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseResolution(%q) = %+v, want %+v", c.in, got, c.want)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("ParseResolution(%q) result fails Validate: %v", c.in, err)
		}
	}
}

func TestParseResolutionErrors(t *testing.T) {
	for _, in := range []string{"", "bogus", "1920x1080x720", "0x0"} {
		if _, err := ParseResolution(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseResolution(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseSpeed(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"2", 2},
		{"2x", 2},
		{"1.5x", 1.5},
	}
	for _, c := range cases {
		got, err := ParseSpeed(c.in)
		if err != nil {
			t.Errorf("ParseSpeed(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.Factor != c.want {
			t.Errorf("ParseSpeed(%q) = %v, want %v", c.in, got.Factor, c.want)
		}
	}
}

func TestParseSpeedErrors(t *testing.T) {
	for _, in := range []string{"", "abc", "x"} {
		if _, err := ParseSpeed(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseSpeed(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseTrim(t *testing.T) {
	cases := []struct {
		in   string
		want Trim
	}{
		{"0:05-0:20", Trim{5, 20}},
		{"5-", Trim{5, 0}},
		{"-20", Trim{0, 20}},
	}
	for _, c := range cases {
		got, err := ParseTrim(c.in)
		if err != nil {
			t.Errorf("ParseTrim(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTrim(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseTrimErrors(t *testing.T) {
	for _, in := range []string{"", "-", "abc"} {
		if _, err := ParseTrim(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseTrim(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseFPS(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"30", 30},
		{"29.97", 29.97},
	}
	for _, c := range cases {
		got, err := ParseFPS(c.in)
		if err != nil {
			t.Errorf("ParseFPS(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.FPS != c.want {
			t.Errorf("ParseFPS(%q) = %v, want %v", c.in, got.FPS, c.want)
		}
	}
}

func TestParseFPSErrors(t *testing.T) {
	for _, in := range []string{"", "abc", "0"} {
		if _, err := ParseFPS(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseFPS(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

// TestParseRejectsNaNAndInf covers the value parsers that accept a raw
// number: strconv.ParseFloat happily parses "nan"/"inf" text into NaN/±Inf,
// so each of these must reject it explicitly rather than silently storing
// a non-finite step value.
func TestParseRejectsNaNAndInf(t *testing.T) {
	for _, n := range []string{"nan", "NaN", "inf", "-inf", "Inf", "+Inf"} {
		if _, err := ParseSpeed(n); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseSpeed(%q) error = %v, want ErrInvalidStep", n, err)
		}
		if _, err := ParseFPS(n); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseFPS(%q) error = %v, want ErrInvalidStep", n, err)
		}
		if _, err := ParseTrim(n + "-"); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseTrim(%q) error = %v, want ErrInvalidStep", n+"-", err)
		}
		if _, err := ParseResolution(n + "%"); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseResolution(%q) error = %v, want ErrInvalidStep", n+"%", err)
		}
	}
}

func TestParseCodec(t *testing.T) {
	cases := map[string]Codec{
		"h264":              CodecH264,
		"h265":              CodecH265,
		"hevc":              CodecH265,
		"av1":               CodecAV1,
		"vp9":               CodecVP9,
		"h264-hw":           CodecH264HW,
		"h265-hw":           CodecH265HW,
		"copy":              CodecCopy,
		"libx264":           CodecH264,
		"libx265":           CodecH265,
		"libsvtav1":         CodecAV1,
		"libvpx-vp9":        CodecVP9,
		"h264_videotoolbox": CodecH264HW,
		"hevc_videotoolbox": CodecH265HW,
	}
	for in, want := range cases {
		got, err := ParseCodec(in)
		if err != nil {
			t.Errorf("ParseCodec(%q) unexpected error: %v", in, err)
			continue
		}
		if got.Codec != want {
			t.Errorf("ParseCodec(%q) = %v, want %v", in, got.Codec, want)
		}
	}
}

func TestParseCodecErrors(t *testing.T) {
	for _, in := range []string{"", "bogus"} {
		if _, err := ParseCodec(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseCodec(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseQuality(t *testing.T) {
	cases := []struct {
		in   string
		want Quality
	}{
		{"23", Quality{CRF: 23}},
		{"crf 23", Quality{CRF: 23}},
		{"crf23", Quality{CRF: 23}},
		{"20MB", Quality{TargetBytes: 20_000_000}},
	}
	for _, c := range cases {
		got, err := ParseQuality(c.in)
		if err != nil {
			t.Errorf("ParseQuality(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseQuality(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseQualityErrors(t *testing.T) {
	for _, in := range []string{"", "abc", "crf"} {
		if _, err := ParseQuality(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseQuality(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseAudio(t *testing.T) {
	cases := []struct {
		in   string
		want Audio
	}{
		{"keep", Audio{Mode: AudioKeep}},
		{"remove", Audio{Mode: AudioRemove}},
		{"aac", Audio{Mode: AudioAAC, BitrateK: 128}},
		{"aac:96k", Audio{Mode: AudioAAC, BitrateK: 96}},
		{"96k", Audio{Mode: AudioAAC, BitrateK: 96}},
	}
	for _, c := range cases {
		got, err := ParseAudio(c.in)
		if err != nil {
			t.Errorf("ParseAudio(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAudio(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseAudioErrors(t *testing.T) {
	for _, in := range []string{"", "bogus", "aac:5k"} {
		if _, err := ParseAudio(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseAudio(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestParseContainer(t *testing.T) {
	cases := map[string]Format{
		"mp4":  FormatMP4,
		".MOV": FormatMOV,
		"mkv":  FormatMKV,
		"webm": FormatWebM,
	}
	for in, want := range cases {
		got, err := ParseContainer(in)
		if err != nil {
			t.Errorf("ParseContainer(%q) unexpected error: %v", in, err)
			continue
		}
		if got.Format != want {
			t.Errorf("ParseContainer(%q) = %v, want %v", in, got.Format, want)
		}
	}
}

func TestParseContainerErrors(t *testing.T) {
	for _, in := range []string{"", "avi"} {
		if _, err := ParseContainer(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseContainer(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"-map_metadata -1", []string{"-map_metadata", "-1"}},
		{`-metadata title="My Clip"`, []string{"-metadata", "title=My Clip"}},
		{`'literal \n text'`, []string{`literal \n text`}},
		{`"a\"b\\c"`, []string{`a"b\c`}},
		{`a\ b`, []string{"a b"}},
		{`-metadata "comment=a\:b"`, []string{"-metadata", `comment=a\:b`}},
		{`"C:\temp"`, []string{`C:\temp`}},
	}
	for _, c := range cases {
		got, err := SplitArgs(c.in)
		if err != nil {
			t.Errorf("SplitArgs(%q) unexpected error: %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("SplitArgs(%q) = %#v, want %#v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("SplitArgs(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestSplitArgsErrors(t *testing.T) {
	for _, in := range []string{`"unterminated`, `'unterminated`, "   ", ""} {
		if _, err := SplitArgs(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("SplitArgs(%q) error = %v, want ErrInvalidStep", in, err)
		}
	}
}
