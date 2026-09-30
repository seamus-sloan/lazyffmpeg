package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func imageSession(pl pipeline.Pipeline) app.Session {
	return app.Session{
		Input:    "photo.jpg",
		Info:     probe.Info{SizeBytes: 2_400_000, Video: probe.VideoStream{Codec: "mjpeg", Width: 4032, Height: 3024}},
		Pipeline: pl,
	}
}

func TestImageMenuOffersOnlyImageSteps(t *testing.T) {
	m := resized(New(imageSession(pipeline.New())), 100, 30)

	want := []pipeline.Kind{pipeline.KindResolution, pipeline.KindCrop, pipeline.KindRotate,
		pipeline.KindFilename, pipeline.KindRawArgs}
	got := m.menuKinds()
	if len(got) != len(want) {
		t.Fatalf("menuKinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("menuKinds = %v, want %v", got, want)
		}
	}
	out := viewText(m)
	for _, s := range []string{"Image", "Resolution", "Crop", "Rotate / flip", "File name", "Raw args", "Run"} {
		if !strings.Contains(out, s) {
			t.Errorf("MENU is missing %q, got:\n%s", s, out)
		}
	}
	for _, s := range []string{"Speed", "Trim", "Encoder", "Audio"} {
		if strings.Contains(out, s) {
			t.Errorf("MENU offers %q for an image, got:\n%s", s, out)
		}
	}
}

func TestImageInfoAndFooterHaveNoTimes(t *testing.T) {
	m := resized(New(imageSession(pipeline.New())), 100, 30)
	out := viewText(m)

	if !strings.Contains(out, "4032×3024 · JPG · 2.4 MB") {
		t.Errorf("info line should give the image's size, format and file size, got:\n%s", out)
	}
	if strings.Contains(out, "00:00") || strings.Contains(out, "--:--") || strings.Contains(out, "~") {
		t.Errorf("an image's screen should show no times or size estimate, got:\n%s", out)
	}
	if !strings.Contains(out, "→ photo (edited).jpg") {
		t.Errorf("footer should name the output, got:\n%s", out)
	}
}

func TestImageIgnoresTimelineKeys(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := New(imageSession(pipeline.New()), WithRenderer(fn, true))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	before := len(*reqs)

	for _, k := range []string{"l", "L", "h", "H", "space", "i", "o"} {
		m = step(t, m, key(k))
	}

	if m.preview.time != 0 || m.preview.playing || m.pipeline.Len() != 0 {
		t.Errorf("time %v, playing %v, %d steps; timeline keys should do nothing for an image",
			m.preview.time, m.preview.playing, m.pipeline.Len())
	}
	if len(*reqs) != before {
		t.Errorf("timeline keys issued %d renders, want none", len(*reqs)-before)
	}
}

func TestImageHelpLeavesOutTimelineKeys(t *testing.T) {
	m := resized(New(imageSession(pipeline.New())), 100, 30)
	m = step(t, m, key("?"))
	out := viewText(m)

	for _, s := range []string{"seek the preview", "play/pause", "trim start/end"} {
		if strings.Contains(out, s) {
			t.Errorf("help mentions %q for an image, got:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "copy the preview's frame") {
		t.Errorf("help is missing the keys that do apply, got:\n%s", out)
	}
}

func TestImageResolutionPresetsLimitTheLongestSide(t *testing.T) {
	m := resized(New(imageSession(pipeline.New())), 100, 30)
	m = openMenu(t, m, pipeline.KindResolution)

	out := viewText(m)
	if !strings.Contains(out, "Longest side 2048") || strings.Contains(out, "3840×2160") {
		t.Errorf("Resolution modal should offer image presets, got:\n%s", out)
	}
	m = step(t, m, key("enter"))
	if s, ok := m.pipeline.Find(pipeline.KindResolution); !ok || s != (pipeline.Resolution{Width: 2048, Height: 2048}) {
		t.Errorf("first preset added %v, want a 2048×2048 fit box", s)
	}
}

func TestCropAndRotateCustomTextRoundTrips(t *testing.T) {
	for _, s := range []pipeline.Step{
		pipeline.Crop{AspectW: 16, AspectH: 9},
		pipeline.Crop{Width: 800, Height: 600},
		pipeline.Crop{Width: 800, Height: 600, X: 10, Y: 20, Offset: true},
		pipeline.Rotate{Degrees: 90},
		pipeline.Rotate{Flip: pipeline.FlipHorizontal},
		pipeline.Rotate{Degrees: 270, Flip: pipeline.FlipVertical},
	} {
		text := customPrefill(s)
		got, err := parseCustom(s.Kind(), text)
		if err != nil || got != s {
			t.Errorf("%+v prefills %q, which parses back to %+v, %v", s, text, got, err)
		}
	}
}

func TestCropAndRotatePresetsAddSteps(t *testing.T) {
	for _, tt := range []struct {
		kind pipeline.Kind
		want pipeline.Step
	}{
		{pipeline.KindCrop, pipeline.Crop{AspectW: 1, AspectH: 1}},
		{pipeline.KindRotate, pipeline.Rotate{Degrees: 90}},
	} {
		for _, session := range []app.Session{imageSession(pipeline.New()), testSession(pipeline.New())} {
			m := resized(New(session), 100, 30)
			m = openMenu(t, m, tt.kind)
			m = step(t, m, key("enter"))
			if got, ok := m.pipeline.Find(tt.kind); !ok || got != tt.want {
				t.Errorf("%s on %s: first preset added %v, want %+v", tt.kind.Label(), session.Input, got, tt.want)
			}
		}
	}
}
