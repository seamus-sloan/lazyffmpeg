package pipeline_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
	"github.com/seamus-sloan/lazyffmpeg/internal/testclip"
)

func runFFmpeg(t *testing.T, argv []string) {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg failed: %v\n%s", err, out)
	}
}

func TestIntegrationSpeedHalvesDuration(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Speed{Factor: 2})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	want := info.Duration / 2
	if diff := outInfo.Duration - want; diff > 0.1 || diff < -0.1 {
		t.Errorf("output duration = %v, want ~%v", outInfo.Duration, want)
	}
	if outInfo.Audio == nil {
		t.Error("output should still have an audio stream after speed change")
	}
}

func TestIntegrationResolutionChangesSize(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Resolution{Width: 160, Height: 120, Exact: true})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	if outInfo.Video.Width != 160 || outInfo.Video.Height != 120 {
		t.Errorf("output dims = %dx%d, want 160x120", outInfo.Video.Width, outInfo.Video.Height)
	}
}

func TestIntegrationResolutionPercent(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Resolution{Percent: 50})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	if outInfo.Video.Width != 160 || outInfo.Video.Height != 120 {
		t.Errorf("output dims = %dx%d, want 160x120", outInfo.Video.Width, outInfo.Video.Height)
	}
}

func TestIntegrationSpeedAndTrim(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Speed{Factor: 2}, pipeline.Trim{Start: 0.25, End: 0.75})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	want := 0.5
	if diff := outInfo.Duration - want; diff > 0.15 || diff < -0.15 {
		t.Errorf("output duration = %v, want ~%v", outInfo.Duration, want)
	}
}

// TestIntegrationResolutionFitInsideBox verifies the fit-inside-box
// behaviour: the result fits inside the box, keeps aspect within 1%, and
// both sides come out even.
func TestIntegrationResolutionFitInsideBox(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 640, Height: 448, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Resolution{Width: 320, Height: 240})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	w, h := outInfo.Video.Width, outInfo.Video.Height
	if w > 320 || h > 240 {
		t.Errorf("output dims = %dx%d, want to fit inside 320x240", w, h)
	}
	if w%2 != 0 || h%2 != 0 {
		t.Errorf("output dims = %dx%d, want both sides even", w, h)
	}
	wantAspect := 640.0 / 448.0
	gotAspect := float64(w) / float64(h)
	if diff := gotAspect - wantAspect; diff > 0.01 || diff < -0.01 {
		t.Errorf("aspect ratio = %v, want ~%v (within 1%%)", gotAspect, wantAspect)
	}
}

func TestIntegrationOutputDimensionsMatchRealEncode(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	cases := []struct {
		name string
		w, h int
		res  pipeline.Resolution
	}{
		{"fit", 642, 480, pipeline.Resolution{Width: 320, Height: 240}},
		{"single side", 642, 480, pipeline.Resolution{Width: 320}},
		{"percent", 640, 480, pipeline.Resolution{Percent: 50}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := testclip.Make(t, testclip.Spec{Width: c.w, Height: c.h, Seconds: 1})
			out := filepath.Join(filepath.Dir(in), "out.mp4")

			info, err := probe.Run(context.Background(), in)
			if err != nil {
				t.Fatalf("probe.Run: %v", err)
			}
			p := pipeline.New(c.res)
			argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			runFFmpeg(t, argv)

			outInfo, err := probe.Run(context.Background(), out)
			if err != nil {
				t.Fatalf("probe.Run(out): %v", err)
			}
			wantW, wantH := pipeline.OutputDimensions(info, p)
			if outInfo.Video.Width != wantW || outInfo.Video.Height != wantH {
				t.Errorf("real encode dims = %dx%d, want OutputDimensions() = %dx%d",
					outInfo.Video.Width, outInfo.Video.Height, wantW, wantH)
			}
		})
	}
}

func TestIntegrationAudioRemove(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1, Audio: true})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Audio{Mode: pipeline.AudioRemove})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	if outInfo.Audio != nil {
		t.Errorf("output should have no audio stream, got %+v", outInfo.Audio)
	}
}

func TestIntegrationContainerWebm(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1, Audio: true})
	out := filepath.Join(filepath.Dir(in), "out.webm")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Container{Format: pipeline.FormatWebM})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	outInfo, err := probe.Run(context.Background(), out)
	if err != nil {
		t.Fatalf("probe.Run(out): %v", err)
	}
	if outInfo.Video.Codec != "vp9" {
		t.Errorf("output video codec = %q, want vp9", outInfo.Video.Codec)
	}
	if outInfo.Audio == nil || outInfo.Audio.Codec != "opus" {
		t.Errorf("output audio = %+v, want opus", outInfo.Audio)
	}
}

func TestIntegrationEncoderH265MP4HasHVC1Tag(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Encoder{Codec: pipeline.CodecH265})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream_tags=handler_name:stream=codec_tag_string",
		"-of", "default=noprint_wrappers=1", out)
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %v\n%s", err, outBytes)
	}
	if !strings.Contains(string(outBytes), "hvc1") {
		t.Errorf("expected hvc1 codec tag, got:\n%s", outBytes)
	}
}

func TestIntegrationTargetSize(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})
	out := filepath.Join(filepath.Dir(in), "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}

	p := pipeline.New(pipeline.Quality{TargetBytes: 150_000})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	runFFmpeg(t, argv)

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	max := int64(150_000 * 1.10)
	if fi.Size() > max {
		t.Errorf("output size = %d, want <= %d", fi.Size(), max)
	}
}
