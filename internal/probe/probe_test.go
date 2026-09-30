package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/testclip"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseScreenrec(t *testing.T) {
	info, err := Parse(readFixture(t, "screenrec.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Duration < 33 || info.Duration >= 34 {
		t.Errorf("Duration = %v, want in [33,34)", info.Duration)
	}
	if info.SizeBytes == 0 {
		t.Error("SizeBytes = 0, want non-zero")
	}
	if info.BitRate == 0 {
		t.Error("BitRate = 0, want non-zero")
	}
	if info.Video.Codec != "hevc" {
		t.Errorf("Video.Codec = %q, want hevc", info.Video.Codec)
	}
	if info.Video.Width != 3652 || info.Video.Height != 2560 {
		t.Errorf("Video dims = %dx%d, want 3652x2560", info.Video.Width, info.Video.Height)
	}
	if info.Video.FPS != 60 {
		t.Errorf("Video.FPS = %v, want 60", info.Video.FPS)
	}
	if info.Audio == nil {
		t.Fatal("Audio = nil, want non-nil")
	}
	if info.Audio.Codec != "aac" {
		t.Errorf("Audio.Codec = %q, want aac", info.Audio.Codec)
	}
	if info.Audio.BitRate == 0 {
		t.Error("Audio.BitRate = 0, want non-zero")
	}
	if info.Audio.Channels != 2 {
		t.Errorf("Audio.Channels = %d, want 2", info.Audio.Channels)
	}
	if info.Audio.SampleRate != 44100 {
		t.Errorf("Audio.SampleRate = %d, want 44100", info.Audio.SampleRate)
	}
}

func TestParseVideoOnly(t *testing.T) {
	info, err := Parse(readFixture(t, "video_only.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Audio != nil {
		t.Errorf("Audio = %+v, want nil", info.Audio)
	}
}

func TestParseRotated(t *testing.T) {
	info, err := Parse(readFixture(t, "rotated.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Video.Width != 1080 || info.Video.Height != 1920 {
		t.Errorf("Video dims = %dx%d, want 1080x1920 (rotated)", info.Video.Width, info.Video.Height)
	}
}

func TestParseCoverArt(t *testing.T) {
	info, err := Parse(readFixture(t, "cover_art.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Video.Codec != "h264" {
		t.Errorf("Video.Codec = %q, want h264 (skip attached_pic stream)", info.Video.Codec)
	}
	if info.Video.Width != 1920 || info.Video.Height != 1080 {
		t.Errorf("Video dims = %dx%d, want 1920x1080", info.Video.Width, info.Video.Height)
	}
}

func TestParseFPSFallback(t *testing.T) {
	info, err := Parse(readFixture(t, "fps_fallback.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Video.FPS != 30 {
		t.Errorf("Video.FPS = %v, want 30 (fallback to r_frame_rate)", info.Video.FPS)
	}
}

func TestParseAudioOnly(t *testing.T) {
	_, err := Parse(readFixture(t, "audio_only.json"))
	if !errors.Is(err, ErrNoVideo) {
		t.Errorf("err = %v, want ErrNoVideo", err)
	}
}

func TestParseMalformed(t *testing.T) {
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Error("Parse(malformed) expected error, got nil")
	}
}

func TestParseDurationFallsBackToStream(t *testing.T) {
	data := []byte(`{
		"streams": [
			{"index":0,"codec_name":"h264","codec_type":"video","width":100,"height":100,
			 "pix_fmt":"yuv420p","r_frame_rate":"30/1","avg_frame_rate":"30/1","duration":"7.500000",
			 "disposition":{"attached_pic":0}}
		],
		"format": {"format_name":"mp4","size":"1000","bit_rate":"1000"}
	}`)
	info, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Duration != 7.5 {
		t.Errorf("Duration = %v, want 7.5", info.Duration)
	}
}

func TestParseDurationBothMissing(t *testing.T) {
	data := []byte(`{
		"streams": [
			{"index":0,"codec_name":"h264","codec_type":"video","width":100,"height":100,
			 "pix_fmt":"yuv420p","r_frame_rate":"30/1","avg_frame_rate":"30/1",
			 "disposition":{"attached_pic":0}}
		],
		"format": {"format_name":"mp4","size":"1000","bit_rate":"1000"}
	}`)
	info, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if info.Duration != 0 {
		t.Errorf("Duration = %v, want 0", info.Duration)
	}
}

func TestRun(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	path := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, FPS: 30, Seconds: 2, Audio: true})

	info, err := Run(context.Background(), path)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if info.Video.Width != 320 || info.Video.Height != 240 {
		t.Errorf("Video dims = %dx%d, want 320x240", info.Video.Width, info.Video.Height)
	}
	if info.Video.FPS != 30 {
		t.Errorf("Video.FPS = %v, want 30", info.Video.FPS)
	}
	if info.Duration < 1.5 || info.Duration > 2.5 {
		t.Errorf("Duration = %v, want ~2", info.Duration)
	}
	if info.Audio == nil {
		t.Fatal("Audio = nil, want non-nil")
	}
}

func TestRunMissingPath(t *testing.T) {
	testclip.RequireTools(t, "ffprobe")
	_, err := Run(context.Background(), "/nonexistent/path/lazyff-does-not-exist.mp4")
	if err == nil {
		t.Fatal("Run(missing path) expected error, got nil")
	}
}

func TestMakeNameWithSpacesAndNarrowSpace(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	path := testclip.Make(t, testclip.Spec{Name: "clip at 1.02 PM.mp4", Seconds: 1})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("clip not created: %v", err)
	}
}

func TestIsImage(t *testing.T) {
	cases := map[string]bool{
		"a.png": true, "a.JPG": true, "a.jpeg": true, "a.avif": true, "a.webp": true,
		"a.bmp": true, "a.tif": true, "a.tiff": true, "a.heic": true, "a.heif": true,
		"a.mp4": false, "a.gif": false, "a.mov": false, "png": false, "": false,
	}
	for path, want := range cases {
		if got := IsImage(path); got != want {
			t.Errorf("IsImage(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRunGivesAnImageNoDuration(t *testing.T) {
	path := testclip.MakeImage(t, "still.jpg", 64, 48)

	info, err := Run(context.Background(), path)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if info.Duration != 0 || info.Video.Width != 64 || info.Video.Height != 48 {
		t.Errorf("info = %+v, want a 64x48 image with no duration", info)
	}
}
