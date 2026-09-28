package pipeline

import "testing"

func TestDefaultOutputPath(t *testing.T) {
	got := DefaultOutputPath("/v/Screen Recording at 1.02 PM.mov", New())
	want := "/v/Screen Recording at 1.02 PM (edited).mov"
	if got != want {
		t.Errorf("DefaultOutputPath = %q, want %q", got, want)
	}
}

func TestDefaultOutputPathNoExtension(t *testing.T) {
	got := DefaultOutputPath("/v/clip", New())
	want := "/v/clip (edited).mp4"
	if got != want {
		t.Errorf("DefaultOutputPath = %q, want %q", got, want)
	}
}

func TestDefaultOutputPathContainerStep(t *testing.T) {
	got := DefaultOutputPath("/v/clip.mov", New(Container{Format: FormatMKV}))
	want := "/v/clip (edited).mkv"
	if got != want {
		t.Errorf("DefaultOutputPath = %q, want %q", got, want)
	}
}

func TestDefaultOutputPathUnsupportedExtensionFallsBackToMP4(t *testing.T) {
	cases := map[string]string{
		"/v/clip.gif": "/v/clip (edited).mp4",
		"/v/clip.qt":  "/v/clip (edited).mp4",
		"/v/clip.MOV": "/v/clip (edited).MOV",
		"/v/clip.mkv": "/v/clip (edited).mkv",
	}
	for input, want := range cases {
		if got := DefaultOutputPath(input, New()); got != want {
			t.Errorf("DefaultOutputPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDefaultOutputPathKeepsM4VOnlyForH264OrCopy(t *testing.T) {
	cases := []struct {
		input string
		codec Codec // "" = no Encoder step
		want  string
	}{
		{"/v/clip.m4v", "", "/v/clip (edited).m4v"},
		{"/v/clip.m4v", CodecH264, "/v/clip (edited).m4v"},
		{"/v/clip.m4v", CodecH264HW, "/v/clip (edited).m4v"},
		{"/v/clip.m4v", CodecCopy, "/v/clip (edited).m4v"},
		{"/v/clip.m4v", CodecH265, "/v/clip (edited).mp4"},
		{"/v/clip.m4v", CodecH265HW, "/v/clip (edited).mp4"},
		{"/v/clip.m4v", CodecAV1, "/v/clip (edited).mp4"},
		{"/v/clip.M4V", CodecVP9, "/v/clip (edited).mp4"},
		{"/v/clip.mp4", CodecH265, "/v/clip (edited).mp4"},
		{"/v/clip.mov", CodecAV1, "/v/clip (edited).mov"},
	}
	for _, c := range cases {
		p := New()
		if c.codec != "" {
			p = New(Encoder{Codec: c.codec})
		}
		if got := DefaultOutputPath(c.input, p); got != c.want {
			t.Errorf("DefaultOutputPath(%q, %q) = %q, want %q", c.input, c.codec, got, c.want)
		}
	}
}

func TestOutputPath(t *testing.T) {
	named := func(name string, steps ...Step) Pipeline {
		return New(append([]Step{Filename{Name: name}}, steps...)...)
	}
	cases := []struct {
		name     string
		input    string
		explicit string // -o; "" when unset
		p        Pipeline
		want     string
	}{
		// A name ending in a container extension lazyff writes is used as is.
		{"mp4 name on a mov input", "/v/clip.mov", "", named("demo.mp4"), "/v/demo.mp4"},
		{"mov name", "/v/clip.mp4", "", named("demo.mov"), "/v/demo.mov"},
		{"mkv name, upper case", "/v/clip.mov", "", named("DEMO.MKV"), "/v/DEMO.MKV"},
		{"webm name, mixed case", "/v/clip.mov", "", named("demo.WebM"), "/v/demo.WebM"},
		{"m4v name", "/v/clip.mp4", "", named("demo.m4v"), "/v/demo.m4v"},
		// Otherwise the default extension is appended.
		{"bare name keeps the input's mov", "/v/clip.mov", "", named("demo"), "/v/demo.mov"},
		{"dotted name keeps the input's mov", "/v/clip.mov", "", named("my.clip"), "/v/my.clip.mov"},
		{"gif name gets mp4", "/v/clip.mp4", "", named("demo.gif"), "/v/demo.gif.mp4"},
		{"gif input falls back to mp4", "/v/clip.gif", "", named("demo"), "/v/demo.mp4"},
		{"container step's extension", "/v/clip.mov", "", named("demo", Container{Format: FormatMKV}), "/v/demo.mkv"},
		{"m4v input with h265 falls back to mp4", "/v/clip.m4v", "", named("demo", Encoder{Codec: CodecH265}), "/v/demo.mp4"},
		{"m4v input with h264 keeps m4v", "/v/clip.m4v", "", named("demo"), "/v/demo.m4v"},
		// Directory: -o's when given, else the input's.
		{"in -o's directory", "/v/clip.mov", "/out/x.mkv", named("demo"), "/out/demo.mov"},
		{"in -o's directory, name with extension", "/v/clip.mov", "/out/x.mkv", named("demo.webm"), "/out/demo.webm"},
		{"relative input", "clip.mov", "", named("demo.mp4"), "demo.mp4"},
		// A name that resolves to the input itself is returned as such (the
		// caller refuses it as the same-as-input case).
		{"name resolving to the input", "/v/clip.mov", "", named("clip"), "/v/clip.mov"},
		// No File name step: -o, else the default "(edited)" name.
		{"-o without a name", "/v/clip.mov", "/out/x.mkv", New(), "/out/x.mkv"},
		{"default", "/v/clip.mov", "", New(), "/v/clip (edited).mov"},
	}
	for _, c := range cases {
		if got := OutputPath(c.input, c.explicit, c.p); got != c.want {
			t.Errorf("%s: OutputPath(%q, %q) = %q, want %q", c.name, c.input, c.explicit, got, c.want)
		}
	}
}

func TestQuoteCommand(t *testing.T) {
	argv := []string{"ffmpeg", "-i", "a b.mov", "out.mp4", ""}
	got := QuoteCommand(argv)
	want := `ffmpeg -i 'a b.mov' out.mp4 ''`
	if got != want {
		t.Errorf("QuoteCommand = %q, want %q", got, want)
	}
}

func TestQuoteCommandBareChars(t *testing.T) {
	argv := []string{"-map_metadata", "-1", "title=x,y:z@1%2/3.4"}
	got := QuoteCommand(argv)
	want := "-map_metadata -1 title=x,y:z@1%2/3.4"
	if got != want {
		t.Errorf("QuoteCommand = %q, want %q", got, want)
	}
}

func TestQuoteCommandEscapesSingleQuote(t *testing.T) {
	argv := []string{"it's"}
	got := QuoteCommand(argv)
	want := `'it'\''s'`
	if got != want {
		t.Errorf("QuoteCommand = %q, want %q", got, want)
	}
}
