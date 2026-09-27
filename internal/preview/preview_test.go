package preview

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/seamus-sloan/lazyffmpeg/internal/testclip"
)

func TestFrameArgs(t *testing.T) {
	got := FrameArgs(Request{Input: "a b.mov", Time: 12.5, Cols: 60, Rows: 20})
	want := []string{
		"ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", "12.5",
		"-i", "a b.mov",
		"-frames:v", "1",
		"-vf", "scale=240:160:force_original_aspect_ratio=decrease",
		"-f", "image2pipe",
		"-c:v", "png",
		"-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FrameArgs = %v, want %v", got, want)
	}
}

func TestFrameArgsWithFilter(t *testing.T) {
	got := FrameArgs(Request{Input: "a.mov", Time: 1, Filter: "scale=1920:1080", Cols: 60, Rows: 20})
	wantVF := "scale=1920:1080,scale=240:160:force_original_aspect_ratio=decrease"
	found := false
	for i, a := range got {
		if a == "-vf" {
			if got[i+1] != wantVF {
				t.Errorf("-vf = %q, want %q", got[i+1], wantVF)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("-vf not present in argv")
	}
}

func TestChafaArgs(t *testing.T) {
	got := ChafaArgs(Request{Cols: 60, Rows: 20, Colors: "256"})
	want := []string{
		"chafa",
		"--format", "symbols",
		"--size", "60x20",
		"--animate", "off",
		"--polite", "on",
		"--colors", "256",
		"-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChafaArgs = %v, want %v", got, want)
	}
}

func TestRenderProducesFrame(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe", "chafa")
	path := testclip.Make(t, testclip.Spec{Width: 64, Height: 48, Seconds: 1})

	out, err := Render(context.Background(), Request{
		Input: path, Time: 0.1, Cols: 20, Rows: 10, Colors: "256",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("Render returned empty text")
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 10 {
		t.Errorf("Render returned %d lines, want at most 10", len(lines))
	}
}

func TestRenderMissingInput(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe", "chafa")
	_, err := Render(context.Background(), Request{
		Input: "/does/not/exist.mp4", Time: 0, Cols: 10, Rows: 5, Colors: "256",
	})
	if err == nil {
		t.Fatal("Render on a missing input: want error, got nil")
	}
	if !strings.Contains(err.Error(), "No such file") {
		t.Errorf("error %q does not look like it carries ffmpeg's stderr", err.Error())
	}
}

func TestRenderCanceledContext(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe", "chafa")
	path := testclip.Make(t, testclip.Spec{Width: 64, Height: 48, Seconds: 1})

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, err := Render(ctx, Request{Input: path, Time: 0, Cols: 10, Rows: 5, Colors: "256"})
	if err == nil {
		t.Fatal("Render with a canceled context: want error, got nil")
	}
}
