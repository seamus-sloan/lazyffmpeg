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

func TestParseConvert(t *testing.T) {
	tests := map[string]Convert{
		"png":      {Format: ImagePNG},
		"JPG":      {Format: ImageJPEG},
		"jpeg 85":  {Format: ImageJPEG, Quality: 85},
		"avif 60%": {Format: ImageAVIF, Quality: 60},
		"tif":      {Format: ImageTIFF},
		"bmp":      {Format: ImageBMP},
	}
	for in, want := range tests {
		got, err := ParseConvert(in)
		if err != nil || got != want {
			t.Errorf("ParseConvert(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "webp", "gif", "jpeg 0", "jpeg 101", "jpeg best", "png 80", "jpeg 80 90"} {
		if _, err := ParseConvert(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseConvert(%q): err = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestConvertSetsTheFormatAndQuality(t *testing.T) {
	tests := []struct {
		c          Convert
		out, codec string
	}{
		{Convert{Format: ImageJPEG}, "in (edited).jpg", "-c:v mjpeg -q:v 2"},
		{Convert{Format: ImageJPEG, Quality: 100}, "in (edited).jpg", "-c:v mjpeg -q:v 2"},
		{Convert{Format: ImageJPEG, Quality: 85}, "in (edited).jpg", "-c:v mjpeg -q:v 6"},
		{Convert{Format: ImageJPEG, Quality: 1}, "in (edited).jpg", "-c:v mjpeg -q:v 31"},
		{Convert{Format: ImageAVIF}, "in (edited).avif", "-c:v libsvtav1 -crf 30 -pix_fmt yuv420p"},
		{Convert{Format: ImageAVIF, Quality: 60}, "in (edited).avif", "-c:v libsvtav1 -crf 25 -pix_fmt yuv420p"},
		{Convert{Format: ImageTIFF}, "in (edited).tif", "-c:v tiff"},
	}
	for _, tt := range tests {
		p := New(tt.c)
		out := OutputPath("in.png", "", p)
		if out != tt.out {
			t.Errorf("%s: output = %q, want %q", tt.c.Summary(), out, tt.out)
		}
		argv, err := Compile(infoImage(), p, Options{Input: "in.png", Output: out})
		if err != nil {
			t.Fatalf("%s: Compile: %v", tt.c.Summary(), err)
		}
		if got := QuoteCommand(argv); !strings.Contains(got, " "+tt.codec+" ") {
			t.Errorf("%s: argv %s, want %s", tt.c.Summary(), got, tt.codec)
		}
	}
}

func TestConvertMustAgreeWithTheOutputsName(t *testing.T) {
	jpeg := Convert{Format: ImageJPEG}
	_, err := Compile(infoImage(), New(jpeg), Options{Input: "in.png", Output: "out.png"})
	if !errors.Is(err, ErrConvertMismatch) || err.Error() != "output does not match the Convert format: out.png is PNG, Convert writes JPEG" {
		t.Errorf("-o out.png with Convert JPEG: err = %v", err)
	}
	p := New(jpeg, Filename{Name: "demo.avif"})
	_, err = Compile(infoImage(), p, Options{Input: "in.png", Output: OutputPath("in.png", "", p)})
	if !errors.Is(err, ErrConvertMismatch) {
		t.Errorf("File name demo.avif with Convert JPEG: err = %v, want ErrConvertMismatch", err)
	}
	p = New(jpeg, Filename{Name: "demo"})
	if got := OutputPath("in.png", "", p); got != "demo.jpg" {
		t.Errorf("a bare File name with Convert JPEG writes %q, want demo.jpg", got)
	}
}

func TestConvertIsListedAmongTheOutputSteps(t *testing.T) {
	p := New(RawArgs{Text: "-y", Args: []string{"-y"}}, Convert{Format: ImagePNG}, Filename{Name: "x"}, Crop{AspectW: 1, AspectH: 1})
	var kinds []Kind
	for _, s := range p.Steps() {
		kinds = append(kinds, s.Kind())
	}
	want := []Kind{KindCrop, KindConvert, KindFilename, KindRawArgs}
	if len(kinds) != len(want) || p.Len() != len(want) {
		t.Fatalf("Steps kinds = %v (Len %d), want %v", kinds, p.Len(), want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("Steps kinds = %v, want %v", kinds, want)
		}
	}
}

func TestConvertIsOnlyForImages(t *testing.T) {
	_, err := Compile(infoNoAudio(10), New(Convert{Format: ImagePNG}), Options{Input: "in.mp4", Output: "out.mp4"})
	if !errors.Is(err, ErrConvertVideo) {
		t.Errorf("err = %v, want ErrConvertVideo", err)
	}
}
