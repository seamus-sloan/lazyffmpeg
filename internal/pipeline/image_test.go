package pipeline

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func infoImage() probe.Info {
	return probe.Info{Video: probe.VideoStream{Codec: "png", Width: 1200, Height: 800}}
}

func TestCompileImageWritesOneFrameInTheOutputsFormat(t *testing.T) {
	tests := []struct {
		output, want string
	}{
		{"out.png", "-c:v png"},
		{"out.JPG", "-c:v mjpeg -q:v 2"},
		{"out.avif", "-c:v libsvtav1 -crf 30 -pix_fmt yuv420p"},
		{"out.tiff", "-c:v tiff"},
		{"out.bmp", "-c:v bmp"},
	}
	for _, tt := range tests {
		argv, err := Compile(infoImage(), New(Resolution{Width: 600}), Options{Input: "in.png", Output: tt.output})
		if err != nil {
			t.Fatalf("Compile to %s: %v", tt.output, err)
		}
		want := "ffmpeg -hide_banner -nostdin -i in.png -map 0:V:0 -vf scale=600:-2 -frames:v 1 " +
			tt.want + " -update 1 " + tt.output
		if got := QuoteCommand(argv); got != want {
			t.Errorf("to %s:\n got %s\nwant %s", tt.output, got, want)
		}
	}
}

func TestCompileImagePutsRawArgsBeforeTheOutput(t *testing.T) {
	p := New(RawArgs{Text: "-compression_level 9", Args: []string{"-compression_level", "9"}})
	argv, err := Compile(infoImage(), p, Options{Input: "in.png", Output: "out.png"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := strings.Join(argv[len(argv)-3:], " "); got != "-compression_level 9 out.png" {
		t.Errorf("argv ends %q, want the raw args right before the output", got)
	}
}

func TestCompileImageRefusesVideoSteps(t *testing.T) {
	for _, s := range []Step{
		Speed{Factor: 2}, Trim{Start: 1}, FrameRate{FPS: 30}, Encoder{Codec: CodecH264},
		Quality{CRF: 20}, Audio{Mode: AudioRemove}, Container{Format: FormatMP4},
	} {
		_, err := Compile(infoImage(), New(s), Options{Input: "in.png", Output: "out.png"})
		if !errors.Is(err, ErrNotForImage) {
			t.Errorf("%s step: err = %v, want ErrNotForImage", s.Kind().Label(), err)
			continue
		}
		if want := s.Kind().Label() + " does not apply to an image"; err.Error() != want {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func TestCompileImageRefusesFormatsItCannotWrite(t *testing.T) {
	for _, out := range []string{"out.webp", "out.heic", "out.mp4", "out"} {
		if _, err := Compile(infoImage(), New(), Options{Input: "in.png", Output: out}); !errors.Is(err, ErrImageFormat) {
			t.Errorf("to %s: err = %v, want ErrImageFormat", out, err)
		}
	}
}

func TestImageOutputPaths(t *testing.T) {
	dir := filepath.Join("some", "dir")
	tests := []struct {
		name, input string
		p           Pipeline
		want        string
	}{
		{"keeps a format lazyff writes", "p.JPG", New(), "p (edited).JPG"},
		{"writes PNG for a format it can only read", "p.webp", New(), "p (edited).png"},
		{"a bare name gets the input's extension", "p.jpg", New(Filename{Name: "demo"}), "demo.jpg"},
		{"a named image extension converts", "p.jpg", New(Filename{Name: "demo.png"}), "demo.png"},
		{"a video extension is just part of the name", "p.png", New(Filename{Name: "demo.mp4"}), "demo.mp4.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutputPath(filepath.Join(dir, tt.input), "", tt.p)
			if want := filepath.Join(dir, tt.want); got != want {
				t.Errorf("OutputPath = %q, want %q", got, want)
			}
		})
	}
}

func TestEstimateHasNoneForAnImage(t *testing.T) {
	_, err := Estimate(infoImage(), New(), Options{Input: "in.png", Output: "out.png"})
	if !errors.Is(err, ErrNoImageEstimate) {
		t.Errorf("err = %v, want ErrNoImageEstimate", err)
	}
}
