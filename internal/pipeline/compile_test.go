package pipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func infoAAC(duration float64) probe.Info {
	return probe.Info{
		Duration: duration,
		Video:    probe.VideoStream{Codec: "h264", Width: 1920, Height: 1080, FPS: 30},
		Audio:    &probe.AudioStream{Codec: "aac", BitRate: 128000, Channels: 2, SampleRate: 44100},
	}
}

func infoNoAudio(duration float64) probe.Info {
	return probe.Info{
		Duration: duration,
		Video:    probe.VideoStream{Codec: "h264", Width: 1920, Height: 1080, FPS: 30},
	}
}

func infoPCM(duration float64) probe.Info {
	return probe.Info{
		Duration: duration,
		Video:    probe.VideoStream{Codec: "h264", Width: 1920, Height: 1080, FPS: 30},
		Audio:    &probe.AudioStream{Codec: "pcm_s16le", BitRate: 1536000, Channels: 2, SampleRate: 48000},
	}
}

func TestCompileEmptyPipeline(t *testing.T) {
	argv, err := Compile(infoAAC(33), New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "ffmpeg -hide_banner -nostdin -i IN -map 0:v:0 -map 0:a:0 " +
		"-c:v libx264 -preset medium -crf 23 -pix_fmt yuv420p " +
		"-c:a copy -movflags +faststart OUT.mp4"
	if got := strings.Join(argv, " "); got != want {
		t.Errorf("Compile argv = %q, want %q", got, want)
	}
}

func TestCompileFiltersJoinInOrder(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080, Exact: true}, Speed{Factor: 2})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-vf scale=1920:1080,setpts=PTS/2") {
		t.Errorf("argv missing expected -vf chain: %s", joined)
	}
	if !strings.Contains(joined, "-af atempo=2.0") {
		t.Errorf("argv missing expected -af chain: %s", joined)
	}
}

func TestCompileSpeed1NoAudioFilter(t *testing.T) {
	p := New(Speed{Factor: 1})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-af") {
		t.Errorf("argv should not contain -af for speed 1: %s", joined)
	}
}

func TestCompileReorderingChangesChain(t *testing.T) {
	p1 := New(Speed{Factor: 2}, Trim{Start: 1, End: 3})
	p2 := New(Trim{Start: 1, End: 3}, Speed{Factor: 2})
	argv1, err := Compile(infoAAC(33), p1, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile p1: %v", err)
	}
	argv2, err := Compile(infoAAC(33), p2, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile p2: %v", err)
	}
	if strings.Join(argv1, " ") == strings.Join(argv2, " ") {
		t.Errorf("reordering did not change the compiled argv")
	}
}

func TestCompileNoInputAudio(t *testing.T) {
	argv, err := Compile(infoNoAudio(10), New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	for _, bad := range []string{"-map 0:a:0", "-af", "-c:a"} {
		if strings.Contains(joined, bad) {
			t.Errorf("argv should not contain %q for no-audio input: %s", bad, joined)
		}
	}
}

func TestCompileAudioFiltersForceAAC(t *testing.T) {
	p := New(Speed{Factor: 2})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:a aac -b:a 128k") {
		t.Errorf("argv missing aac re-encode when audio filters present: %s", joined)
	}
}

func TestCompileAudioCopyUnsuitableForcesAAC(t *testing.T) {
	argv, err := Compile(infoPCM(10), New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:a aac -b:a 128k") {
		t.Errorf("argv should re-encode pcm audio into mp4: %s", joined)
	}
}

func TestCompileAudioCopySuitableInMKV(t *testing.T) {
	argv, err := Compile(infoPCM(10), New(), Options{Input: "IN", Output: "OUT.mkv"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:a copy") {
		t.Errorf("argv should copy any audio codec into mkv: %s", joined)
	}
}

func TestCompileMovflagsOnlyMP4Mov(t *testing.T) {
	argvMP4, _ := Compile(infoAAC(10), New(), Options{Input: "IN", Output: "OUT.mp4"})
	if !strings.Contains(strings.Join(argvMP4, " "), "-movflags +faststart") {
		t.Error("mp4 output should have -movflags +faststart")
	}
	argvMKV, _ := Compile(infoAAC(10), New(), Options{Input: "IN", Output: "OUT.mkv"})
	if strings.Contains(strings.Join(argvMKV, " "), "-movflags") {
		t.Error("mkv output should not have -movflags")
	}
}

func TestCompilePathsWithSpacesVerbatim(t *testing.T) {
	in := "in put  PM.mov"
	out := "out put  PM.mp4"
	argv, err := Compile(infoAAC(10), New(), Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if argv[len(argv)-1] != out {
		t.Errorf("last argv element = %q, want output %q", argv[len(argv)-1], out)
	}
	found := false
	for _, a := range argv {
		if a == in {
			found = true
		}
	}
	if !found {
		t.Errorf("argv does not contain input path verbatim: %v", argv)
	}
}

func TestCompileTrimRangeError(t *testing.T) {
	p := New(Speed{Factor: 2}, Trim{Start: 20})
	_, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrTrimRange) {
		t.Errorf("err = %v, want ErrTrimRange", err)
	}
}

func TestCompileInvalidStepError(t *testing.T) {
	p := New(Speed{Factor: 1000})
	_, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrInvalidStep) {
		t.Errorf("err = %v, want ErrInvalidStep", err)
	}
}

func TestValidateMirrorsCompile(t *testing.T) {
	p := New(Speed{Factor: 2}, Trim{Start: 20})
	if err := Validate(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"}); !errors.Is(err, ErrTrimRange) {
		t.Errorf("Validate err = %v, want ErrTrimRange", err)
	}
	if err := Validate(infoAAC(33), New(), Options{Input: "IN", Output: "OUT.mp4"}); err != nil {
		t.Errorf("Validate err = %v, want nil", err)
	}
}
