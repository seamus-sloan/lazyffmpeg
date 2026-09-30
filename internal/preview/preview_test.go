package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand/v2"
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

func TestFrameArgsForImage(t *testing.T) {
	got := FrameArgs(Request{Input: "a.mov", Time: 1, Filter: "scale=1920:1080", Cols: 60, Rows: 20, ImageID: 1, PixelWidth: 600, PixelHeight: 400})
	wantVF := "scale=1920:1080,scale='min(iw,600)':'min(ih,400)':force_original_aspect_ratio=decrease"
	for i, a := range got {
		if a == "-vf" {
			if got[i+1] != wantVF {
				t.Errorf("-vf = %q, want %q", got[i+1], wantVF)
			}
			return
		}
	}
	t.Fatal("-vf not present in argv")
}

func TestFitCells(t *testing.T) {
	box := Request{Cols: 60, Rows: 20, PixelWidth: 600, PixelHeight: 400} // 10x20 px cells
	tests := []struct {
		name       string
		w, h       int
		cols, rows int
	}{
		{"wider than the box: fills its width", 640, 360, 60, 17},
		{"smaller than the box: scaled up to fill its height", 320, 240, 53, 20},
		{"a sliver still covers one row", 1000, 10, 60, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows := FitCells(tt.w, tt.h, box)
			if cols != tt.cols || rows != tt.rows {
				t.Errorf("FitCells(%d, %d) = %dx%d, want %dx%d", tt.w, tt.h, cols, rows, tt.cols, tt.rows)
			}
		})
	}
}

// kittyChunks splits a sequence of kitty graphics escapes into each one's
// options and payload.
func kittyChunks(t *testing.T, seq string) (opts []string, payloads []string) {
	t.Helper()
	for _, esc := range strings.SplitAfter(seq, "\x1b\\") {
		if esc == "" {
			continue
		}
		body, ok := strings.CutPrefix(esc, "\x1b_G")
		if !ok {
			t.Fatalf("escape %q does not start with APC G", esc)
		}
		body = strings.TrimSuffix(body, "\x1b\\")
		o, p, _ := strings.Cut(body, ";")
		opts = append(opts, o)
		payloads = append(payloads, p)
	}
	return opts, payloads
}

func TestKittyImageChunksThePNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Uint32()) // noise, so the PNG spans several chunks
	}
	var frame bytes.Buffer
	if err := png.Encode(&frame, img); err != nil {
		t.Fatal(err)
	}

	seq, err := KittyImage(Request{Cols: 20, Rows: 10, PixelWidth: 200, PixelHeight: 200, ImageID: 7}, frame.Bytes())
	if err != nil {
		t.Fatalf("KittyImage: %v", err)
	}
	opts, payloads := kittyChunks(t, seq)
	if len(opts) < 2 {
		t.Fatalf("got %d chunks, want several for a %d-byte PNG", len(opts), frame.Len())
	}
	if want := "a=T,f=100,q=2,C=1,i=7,c=20,r=8,m=1"; opts[0] != want {
		t.Errorf("first chunk options = %q, want %q", opts[0], want)
	}
	for i, o := range opts[1:] {
		want := "m=1"
		if i == len(opts)-2 {
			want = "m=0"
		}
		if o != want {
			t.Errorf("chunk %d options = %q, want %q", i+1, o, want)
		}
	}
	for i, p := range payloads {
		if len(p) > kittyChunkSize {
			t.Errorf("chunk %d carries %d bytes, want at most %d", i, len(p), kittyChunkSize)
		}
	}
	got, err := base64.StdEncoding.DecodeString(strings.Join(payloads, ""))
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	if !bytes.Equal(got, frame.Bytes()) {
		t.Error("the chunks' payload does not decode back to the PNG")
	}
}

func TestKittyImageRejectsNonPNG(t *testing.T) {
	if _, err := KittyImage(Request{Cols: 20, Rows: 10, PixelWidth: 200, PixelHeight: 200, ImageID: 1}, []byte("not a png")); err == nil {
		t.Fatal("KittyImage on non-PNG bytes: want error, got nil")
	}
}

func TestDeleteKittyImage(t *testing.T) {
	if got, want := DeleteKittyImage(5), "\x1b_Ga=d,d=I,i=5,q=2\x1b\\"; got != want {
		t.Errorf("DeleteKittyImage(5) = %q, want %q", got, want)
	}
}

func TestRenderImageProducesKittySequence(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	path := testclip.Make(t, testclip.Spec{Width: 64, Height: 48, Seconds: 1})

	seq, err := Render(context.Background(), Request{
		Input: path, Time: 0.1, Cols: 20, Rows: 10, ImageID: 3, PixelWidth: 200, PixelHeight: 200,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	opts, payloads := kittyChunks(t, seq)
	// The 64x48 frame is not scaled up, but is displayed over the 20
	// columns (200px) it fills at 10x20 px cells: 150px, 7.5 rows.
	if !strings.HasPrefix(opts[0], "a=T,f=100,q=2,C=1,i=3,c=20,r=8,") {
		t.Errorf("first chunk options = %q", opts[0])
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(payloads, ""))
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not a PNG: %v", err)
	}
	if cfg.Width != 64 || cfg.Height != 48 {
		t.Errorf("transmitted frame is %dx%d, want the clip's own 64x48", cfg.Width, cfg.Height)
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
