package pipeline

import (
	"errors"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func TestParseCrop(t *testing.T) {
	tests := map[string]Crop{
		"16:9":          {AspectW: 16, AspectH: 9},
		" 1:1 ":         {AspectW: 1, AspectH: 1},
		"800x600":       {Width: 800, Height: 600},
		"800×600+10+20": {Width: 800, Height: 600, X: 10, Y: 20, Offset: true},
		"800X600+0+0":   {Width: 800, Height: 600, Offset: true},
	}
	for in, want := range tests {
		got, err := ParseCrop(in)
		if err != nil || got != want {
			t.Errorf("ParseCrop(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "16:", ":9", "0:9", "abc", "800x", "800x600+10", "1x600", "800x600-5-5"} {
		if _, err := ParseCrop(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseCrop(%q): err = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestCropFiltersAndSummaries(t *testing.T) {
	tests := []struct {
		c               Crop
		filter, summary string
	}{
		{Crop{AspectW: 16, AspectH: 9}, "crop='trunc(min(iw,ih*16/9)/2)*2':'trunc(min(ih,iw*9/16)/2)*2'", "16:9"},
		{Crop{Width: 801, Height: 600}, "crop=800:600", "800×600 centered"},
		{Crop{Width: 800, Height: 600, X: 10, Y: 20, Offset: true}, "crop=800:600:10:20", "800×600 at 10,20"},
	}
	for _, tt := range tests {
		if got := tt.c.VideoFilter(); got != tt.filter {
			t.Errorf("%+v VideoFilter = %q, want %q", tt.c, got, tt.filter)
		}
		if got := tt.c.Summary(); got != tt.summary {
			t.Errorf("%+v Summary = %q, want %q", tt.c, got, tt.summary)
		}
	}
}

func TestParseRotate(t *testing.T) {
	tests := map[string]Rotate{
		"90":   {Degrees: 90},
		"180°": {Degrees: 180},
		"-90":  {Degrees: 270},
		"h":    {Flip: FlipHorizontal},
		"V":    {Flip: FlipVertical},
		"90 h": {Degrees: 90, Flip: FlipHorizontal},
	}
	for in, want := range tests {
		got, err := ParseRotate(in)
		if err != nil || got != want {
			t.Errorf("ParseRotate(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "45", "h 90", "x", "90 h v"} {
		if _, err := ParseRotate(in); !errors.Is(err, ErrInvalidStep) {
			t.Errorf("ParseRotate(%q): err = %v, want ErrInvalidStep", in, err)
		}
	}
}

func TestRotateFiltersAndSummaries(t *testing.T) {
	tests := []struct {
		r               Rotate
		filter, summary string
	}{
		{Rotate{Degrees: 90}, "transpose=clock", "90° clockwise"},
		{Rotate{Degrees: 180}, "hflip,vflip", "180°"},
		{Rotate{Degrees: 270, Flip: FlipHorizontal}, "transpose=cclock,hflip", "90° counter-clockwise, flip horizontal"},
		{Rotate{Flip: FlipVertical}, "vflip", "flip vertical"},
	}
	for _, tt := range tests {
		if got := tt.r.VideoFilter(); got != tt.filter {
			t.Errorf("%+v VideoFilter = %q, want %q", tt.r, got, tt.filter)
		}
		if got := tt.r.Summary(); got != tt.summary {
			t.Errorf("%+v Summary = %q, want %q", tt.r, got, tt.summary)
		}
	}
}

func TestOutputDimensionsWalksCropAndRotateInOrder(t *testing.T) {
	info := probe.Info{Video: probe.VideoStream{Width: 1920, Height: 1080}}
	tests := []struct {
		name string
		p    Pipeline
		w, h int
	}{
		{"square crop", New(Crop{AspectW: 1, AspectH: 1}), 1080, 1080},
		{"portrait crop", New(Crop{AspectW: 9, AspectH: 16}), 606, 1080},
		{"quarter turn swaps the sides", New(Rotate{Degrees: 90}), 1080, 1920},
		{"crop, then scale", New(Crop{Width: 1000, Height: 500}, Resolution{Percent: 50}), 500, 250},
		{"turn, then fit a box", New(Rotate{Degrees: 270}, Resolution{Width: 1000, Height: 1000}), 562, 1000},
	}
	for _, tt := range tests {
		if w, h := OutputDimensions(info, tt.p); w != tt.w || h != tt.h {
			t.Errorf("%s: OutputDimensions = %dx%d, want %dx%d", tt.name, w, h, tt.w, tt.h)
		}
	}
}

func TestCompileRefusesACropOutsideTheFrame(t *testing.T) {
	p := New(Resolution{Width: 640}, Crop{Width: 400, Height: 300, X: 300, Y: 0, Offset: true})
	_, err := Compile(infoNoAudio(10), p, Options{Input: "in.mp4", Output: "out.mp4"})
	if !errors.Is(err, ErrCropOutside) {
		t.Fatalf("err = %v, want ErrCropOutside", err)
	}
	if want := "crop is outside the frame: 400×300 at 300,0 does not fit the 640×360 frame"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestSpatialVideoFilterKeepsSpatialStepsInOrder(t *testing.T) {
	p := New(Rotate{Degrees: 90}, Speed{Factor: 2}, Crop{AspectW: 1, AspectH: 1}, Resolution{Percent: 50})
	want := "transpose=clock,crop='trunc(min(iw,ih*1/1)/2)*2':'trunc(min(ih,iw*1/1)/2)*2',scale=trunc(iw*0.5/2)*2:-2"
	if got := SpatialVideoFilter(p); got != want {
		t.Errorf("SpatialVideoFilter = %q, want %q", got, want)
	}
}

func TestCompileImageAcceptsCropAndRotate(t *testing.T) {
	p := New(Crop{AspectW: 1, AspectH: 1}, Rotate{Flip: FlipHorizontal})
	argv, err := Compile(infoImage(), p, Options{Input: "in.png", Output: "out.png"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "ffmpeg -hide_banner -nostdin -i in.png -map 0:V:0 -vf " +
		"'crop='\\''trunc(min(iw,ih*1/1)/2)*2'\\'':'\\''trunc(min(ih,iw*1/1)/2)*2'\\'',hflip' " +
		"-frames:v 1 -c:v png -update 1 out.png"
	if got := QuoteCommand(argv); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}
