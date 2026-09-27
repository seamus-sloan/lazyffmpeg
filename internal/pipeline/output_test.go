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
