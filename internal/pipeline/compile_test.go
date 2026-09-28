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
	want := "ffmpeg -hide_banner -nostdin -i IN -map 0:V:0 -map 0:a:0 " +
		"-c:v libx264 -preset medium -crf 23 -pix_fmt yuv420p " +
		"-c:a copy -movflags +faststart OUT.mp4"
	if got := strings.Join(argv, " "); got != want {
		t.Errorf("Compile argv = %q, want %q", got, want)
	}
}

func TestCompileMapsVideoStreamExcludingAttachedPictures(t *testing.T) {
	argv, err := Compile(infoAAC(10), New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-map 0:V:0") {
		t.Errorf("argv should map the video stream with capital V (excludes attached-picture streams): %v", argv)
	}
	if strings.Contains(joined, "-map 0:v:0") {
		t.Errorf("argv should not use lowercase v (includes attached pictures): %v", argv)
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

// --- encoder table, CRF, target size, webm, container, raw args ---

func TestCompileEncoderDefaultCRF(t *testing.T) {
	cases := []struct {
		codec Codec
		want  string
	}{
		{CodecH264, "-c:v libx264 -preset medium -crf 23 -pix_fmt yuv420p"},
		{CodecH265, "-c:v libx265 -preset medium -crf 28 -pix_fmt yuv420p -tag:v hvc1"},
		{CodecAV1, "-c:v libsvtav1 -preset 8 -crf 35 -pix_fmt yuv420p"},
		{CodecVP9, "-c:v libvpx-vp9 -deadline good -cpu-used 4 -row-mt 1 -crf 33 -b:v 0 -pix_fmt yuv420p"},
		{CodecH264HW, "-c:v h264_videotoolbox -q:v 66 -pix_fmt yuv420p"},
		{CodecH265HW, "-c:v hevc_videotoolbox -q:v 66 -pix_fmt yuv420p -tag:v hvc1"},
	}
	for _, c := range cases {
		p := New(Encoder{Codec: c.codec})
		argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
		if err != nil {
			t.Errorf("Compile(%v): %v", c.codec, err)
			continue
		}
		joined := strings.Join(argv, " ")
		if !strings.Contains(joined, c.want) {
			t.Errorf("Compile(%v) = %q, want to contain %q", c.codec, joined, c.want)
		}
	}
}

func TestCompileHardwareCRFMapsToQV(t *testing.T) {
	cases := []struct {
		crf  int
		want string
	}{
		{0, "-q:v 100"},
		{51, "-q:v 24"},
	}
	for _, c := range cases {
		p := New(Encoder{Codec: CodecH264HW}, Quality{CRF: c.crf})
		argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
		if err != nil {
			t.Fatalf("Compile(CRF %d): %v", c.crf, err)
		}
		if !strings.Contains(strings.Join(argv, " "), c.want) {
			t.Errorf("Compile(CRF %d) missing %q: %v", c.crf, c.want, argv)
		}
	}
}

func TestCompileQualityCRFReplacesDefault(t *testing.T) {
	p := New(Encoder{Codec: CodecH264}, Quality{CRF: 30})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-crf 30") {
		t.Errorf("argv should use CRF 30, got %v", argv)
	}
}

func TestCompileCRFRangeError(t *testing.T) {
	p := New(Encoder{Codec: CodecH264}, Quality{CRF: 60})
	_, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrCRFRange) {
		t.Errorf("err = %v, want ErrCRFRange", err)
	}
}

func TestCompileCRFRangeOKForVP9(t *testing.T) {
	p := New(Encoder{Codec: CodecVP9}, Quality{CRF: 60})
	_, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Errorf("Compile: unexpected error %v", err)
	}
}

func TestCompileCopyVideoArgs(t *testing.T) {
	p := New(Encoder{Codec: CodecCopy})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:v copy") {
		t.Errorf("argv missing -c:v copy: %s", joined)
	}
}

func TestCompileCopyWithFiltersError(t *testing.T) {
	p := New(Speed{Factor: 2}, Encoder{Codec: CodecCopy})
	_, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrCopyWithFilters) {
		t.Errorf("err = %v, want ErrCopyWithFilters", err)
	}
}

func TestCompileCopyWithQualityError(t *testing.T) {
	p := New(Encoder{Codec: CodecCopy}, Quality{CRF: 23})
	_, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrCopyWithQuality) {
		t.Errorf("err = %v, want ErrCopyWithQuality", err)
	}
}

func infoHEVC(duration float64) probe.Info {
	return probe.Info{
		Duration: duration,
		Video:    probe.VideoStream{Codec: "hevc", Width: 1920, Height: 1080, FPS: 30},
	}
}

func TestCompileHVC1TagForCopyOfHEVC(t *testing.T) {
	p := New(Encoder{Codec: CodecCopy})
	argv, err := Compile(infoHEVC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-tag:v hvc1") {
		t.Errorf("argv missing -tag:v hvc1 for hevc copy into mp4: %v", argv)
	}
}

func TestCompileHVC1TagOnlyMP4Mov(t *testing.T) {
	p := New(Encoder{Codec: CodecH265})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mkv"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if strings.Contains(strings.Join(argv, " "), "-tag:v hvc1") {
		t.Errorf("argv should not have -tag:v hvc1 for mkv: %v", argv)
	}
}

func TestCompileTargetSizeBitrate(t *testing.T) {
	p := New(Speed{Factor: 2}, Quality{TargetBytes: 20_000_000})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "-b:v 9084k -maxrate 9084k -bufsize 18168k"
	if !strings.Contains(strings.Join(argv, " "), want) {
		t.Errorf("argv missing target bitrate %q: %v", want, argv)
	}
}

func TestCompileTargetSizeAV1OmitsMaxrateBufsize(t *testing.T) {
	p := New(Encoder{Codec: CodecAV1}, Quality{TargetBytes: 20_000_000})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-maxrate") || strings.Contains(joined, "-bufsize") {
		t.Errorf("libsvtav1 target mode should omit -maxrate/-bufsize: %v", argv)
	}
	if !strings.Contains(joined, "-b:v") {
		t.Errorf("argv missing -b:v: %v", argv)
	}
}

func TestCompileTargetSizeVP9OmitsCRFModeBZero(t *testing.T) {
	p := New(Encoder{Codec: CodecVP9}, Quality{TargetBytes: 20_000_000})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-b:v 0") {
		t.Errorf("vp9 target mode should not carry the CRF-mode's -b:v 0: %v", argv)
	}
	want := "-b:v 4478k -maxrate 4478k -bufsize 8956k"
	if !strings.Contains(joined, want) {
		t.Errorf("argv missing target bitrate %q: %v", want, argv)
	}
}

func TestCompileTargetSizeAudioRemovedSubtractsNothing(t *testing.T) {
	p := New(Audio{Mode: AudioRemove}, Quality{TargetBytes: 20_000_000})
	argv, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// No audio bitrate subtracted: V = floor(20e6*8*0.95/33/1000) = 4606.
	want := "-b:v 4606k -maxrate 4606k -bufsize 9212k"
	if !strings.Contains(strings.Join(argv, " "), want) {
		t.Errorf("argv missing target bitrate %q: %v", want, argv)
	}
}

func TestCompileTargetSizeAudioCopiedSubtractsInputBitrate(t *testing.T) {
	info := infoAAC(33)
	info.Audio.BitRate = 96000
	p := New(Quality{TargetBytes: 20_000_000})
	argv, err := Compile(info, p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:a copy") {
		t.Fatalf("expected audio copy, got: %v", argv)
	}
	// V = floor((20e6*8*0.95/33 - 96000)/1000) = 4510.
	want := "-b:v 4510k -maxrate 4510k -bufsize 9020k"
	if !strings.Contains(joined, want) {
		t.Errorf("argv missing target bitrate %q: %v", want, argv)
	}
}

func TestCompileTargetTooSmallError(t *testing.T) {
	p := New(Quality{TargetBytes: 1000})
	_, err := Compile(infoAAC(33), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrTargetTooSmall) {
		t.Errorf("err = %v, want ErrTargetTooSmall", err)
	}
}

func TestCompileUnknownDurationError(t *testing.T) {
	p := New(Quality{TargetBytes: 20_000_000})
	_, err := Compile(infoAAC(0), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrUnknownDuration) {
		t.Errorf("err = %v, want ErrUnknownDuration", err)
	}
}

func TestCompileAudioRemove(t *testing.T) {
	p := New(Audio{Mode: AudioRemove})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-an") {
		t.Errorf("argv missing -an: %s", joined)
	}
	if strings.Contains(joined, "-map 0:a:0") {
		t.Errorf("argv should not map audio when removed: %s", joined)
	}
}

func TestCompileAudioAACBitrate(t *testing.T) {
	p := New(Audio{Mode: AudioAAC, BitrateK: 96})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-c:a aac -b:a 96k") {
		t.Errorf("argv missing aac 96k: %v", argv)
	}
}

func TestCompileAudioStepNoInputAudioNoError(t *testing.T) {
	p := New(Audio{Mode: AudioRemove})
	argv, err := Compile(infoNoAudio(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-an") || strings.Contains(joined, "-c:a") || strings.Contains(joined, "-map 0:a:0") {
		t.Errorf("argv should have no audio args when input has no audio: %s", joined)
	}
}

func TestCompileContainerStepOverridesExtension(t *testing.T) {
	p := New(Container{Format: FormatMKV})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-f matroska") {
		t.Errorf("argv missing -f matroska: %s", joined)
	}
	if strings.Contains(joined, "-movflags") {
		t.Errorf("mkv container should not have -movflags: %s", joined)
	}
}

func TestCompileContainerStepMuxerNames(t *testing.T) {
	cases := map[Format]string{
		FormatMP4:  "-f mp4",
		FormatMOV:  "-f mov",
		FormatMKV:  "-f matroska",
		FormatWebM: "-f webm",
	}
	for f, want := range cases {
		p := New(Container{Format: f})
		info := infoAAC(10)
		if f == FormatWebM {
			p = New(Container{Format: f}, Encoder{Codec: CodecVP9})
			info.Audio.Codec = "opus"
		}
		argv, err := Compile(info, p, Options{Input: "IN", Output: "OUT.mp4"})
		if err != nil {
			t.Errorf("Compile(%v): %v", f, err)
			continue
		}
		if !strings.Contains(strings.Join(argv, " "), want) {
			t.Errorf("Compile(%v) missing %q: %v", f, want, argv)
		}
	}
}

func infoVP9Webm(duration float64) probe.Info {
	return probe.Info{
		Duration: duration,
		Video:    probe.VideoStream{Codec: "vp9", Width: 1920, Height: 1080, FPS: 30},
		Audio:    &probe.AudioStream{Codec: "opus", BitRate: 96000, Channels: 2, SampleRate: 48000},
	}
}

func TestCompileWebmDefaultsToVP9(t *testing.T) {
	p := New(Container{Format: FormatWebM})
	argv, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-c:v libvpx-vp9") {
		t.Errorf("argv should default to libvpx-vp9 for webm: %v", argv)
	}
}

func TestCompileWebmInferredFromOutputExtension(t *testing.T) {
	// No Container step at all: the .webm output extension alone selects
	// the webm container (and its libvpx-vp9 default, since there is no
	// Encoder step either).
	argv, err := Compile(infoVP9Webm(10), New(), Options{Input: "IN", Output: "OUT.webm"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:v libvpx-vp9") {
		t.Errorf("argv should default to libvpx-vp9 for a .webm output: %v", argv)
	}
	if !strings.Contains(joined, "-c:a copy") {
		t.Errorf("argv should copy the opus audio for a .webm output: %v", argv)
	}
}

func TestCompileWebmAudioFiltersForceLibopusEvenForOpusInput(t *testing.T) {
	// The input's audio is already opus (normally copied straight
	// through), but a Speed step's atempo chain means the audio stream
	// must be re-encoded, so it cannot simply be copied.
	p := New(Speed{Factor: 2}, Container{Format: FormatWebM})
	argv, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-c:a libopus -b:a 128k") {
		t.Errorf("argv should re-encode to libopus when audio filters apply: %v", argv)
	}
	if strings.Contains(joined, "-c:a copy") {
		t.Errorf("argv should not copy audio when audio filters apply: %v", argv)
	}
}

func TestCompileWebmWrongEncoderError(t *testing.T) {
	p := New(Container{Format: FormatWebM}, Encoder{Codec: CodecH264})
	_, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrWebMVideo) {
		t.Errorf("err = %v, want ErrWebMVideo", err)
	}
}

func TestCompileWebmCopyRequiresVP9OrAV1Input(t *testing.T) {
	p := New(Container{Format: FormatWebM}, Encoder{Codec: CodecCopy})
	_, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Errorf("Compile: unexpected error for vp9 input copy into webm: %v", err)
	}

	nonVP9 := infoVP9Webm(10)
	nonVP9.Video.Codec = "h264"
	_, err = Compile(nonVP9, p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrWebMVideo) {
		t.Errorf("err = %v, want ErrWebMVideo for non-vp9/av1 copy source", err)
	}
}

func TestCompileWebmKeepAudioCopiesOpus(t *testing.T) {
	p := New(Container{Format: FormatWebM})
	argv, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-c:a copy") {
		t.Errorf("argv should copy opus audio into webm: %v", argv)
	}
}

func TestCompileWebmKeepAudioReencodesNonOpus(t *testing.T) {
	info := infoVP9Webm(10)
	info.Audio.Codec = "aac"
	p := New(Container{Format: FormatWebM})
	argv, err := Compile(info, p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "-c:a libopus -b:a 128k") {
		t.Errorf("argv should re-encode non-opus audio to libopus for webm: %v", argv)
	}
}

func TestCompileWebmAACAudioError(t *testing.T) {
	p := New(Container{Format: FormatWebM}, Audio{Mode: AudioAAC, BitrateK: 128})
	_, err := Compile(infoVP9Webm(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrWebMAudio) {
		t.Errorf("err = %v, want ErrWebMAudio", err)
	}
}

func TestCompileRawArgsBeforeOutput(t *testing.T) {
	p := New(RawArgs{Text: "-map_metadata -1", Args: []string{"-map_metadata", "-1"}})
	argv, err := Compile(infoAAC(10), p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(argv) < 3 {
		t.Fatalf("argv too short: %v", argv)
	}
	last := argv[len(argv)-1]
	if last != "OUT.mp4" {
		t.Errorf("last argv element = %q, want output path", last)
	}
	secondLast := argv[len(argv)-2]
	thirdLast := argv[len(argv)-3]
	if thirdLast != "-map_metadata" || secondLast != "-1" {
		t.Errorf("raw args not immediately before output: %v", argv)
	}
}

func TestCompileFilenameAddsNoArgsAndItsExtensionPicksTheContainer(t *testing.T) {
	p := New(Filename{Name: "demo.mp4"})
	out := OutputPath("/v/clip.mov", "", p)

	argv, err := Compile(infoAAC(10), p, Options{Input: "/v/clip.mov", Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// Exactly an empty pipeline's argv, written to /v/demo.mp4: the step
	// itself adds nothing, and the name's extension (not the .mov input's)
	// decides the container, with no -f.
	want := "ffmpeg -hide_banner -nostdin -i /v/clip.mov -map 0:V:0 -map 0:a:0 " +
		"-c:v libx264 -preset medium -crf 23 -pix_fmt yuv420p " +
		"-c:a copy -movflags +faststart /v/demo.mp4"
	if got := strings.Join(argv, " "); got != want {
		t.Errorf("Compile argv = %q, want %q", got, want)
	}
	if got := EffectiveContainer(out, p); got != FormatMP4 {
		t.Errorf("EffectiveContainer(%q) = %v, want FormatMP4", out, got)
	}
}

func TestCompileFilenameExtensionConflictingWithContainerErrors(t *testing.T) {
	p := New(Container{Format: FormatMP4}, Filename{Name: "demo.mov"})
	_, err := Compile(infoAAC(10), p, Options{Input: "/v/clip.mov", Output: OutputPath("/v/clip.mov", "", p)})
	if !errors.Is(err, ErrFilenameContainer) {
		t.Fatalf("err = %v, want ErrFilenameContainer", err)
	}
	for _, want := range []string{"demo.mov", "mp4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestCompileFilenameAgreeingWithContainerIsFine(t *testing.T) {
	cases := []Pipeline{
		New(Container{Format: FormatMKV}, Filename{Name: "DEMO.MKV"}),
		New(Container{Format: FormatMP4}, Filename{Name: "demo.m4v"}), // .m4v is mp4
		New(Container{Format: FormatMKV}, Filename{Name: "demo"}),     // .mkv gets appended
		New(Container{Format: FormatMKV}, Filename{Name: "demo.gif"}), // .mkv gets appended
	}
	for _, p := range cases {
		out := OutputPath("/v/clip.mov", "", p)
		if _, err := Compile(infoAAC(10), p, Options{Input: "/v/clip.mov", Output: out}); err != nil {
			t.Errorf("Compile(%v) to %q: %v, want nil", p.Steps(), out, err)
		}
	}
}

func TestCompileM4VFilenameNeedsAnEncoderM4VCanHold(t *testing.T) {
	cases := []struct {
		codec Codec // "" = no Encoder step
		ok    bool
	}{
		{"", true},
		{CodecH264, true},
		{CodecH264HW, true},
		{CodecCopy, true},
		{CodecH265, false},
		{CodecH265HW, false},
		{CodecAV1, false},
	}
	for _, c := range cases {
		p := New(Filename{Name: "demo.M4V"})
		if c.codec != "" {
			p = p.Upsert(Encoder{Codec: c.codec})
		}
		_, err := Compile(infoAAC(10), p, Options{Input: "/v/clip.mov", Output: OutputPath("/v/clip.mov", "", p)})
		if c.ok && err != nil {
			t.Errorf("demo.M4V with %q: err = %v, want nil", c.codec, err)
		}
		if !c.ok && !errors.Is(err, ErrFilenameM4V) {
			t.Errorf("demo.M4V with %q: err = %v, want ErrFilenameM4V", c.codec, err)
		}
	}
}

func TestEffectiveCodecDefaultsToWebmVP9(t *testing.T) {
	if got := EffectiveCodec(New(), FormatWebM); got != CodecVP9 {
		t.Errorf("EffectiveCodec(no encoder, webm) = %v, want CodecVP9", got)
	}
	if got := EffectiveCodec(New(), FormatMP4); got != CodecH264 {
		t.Errorf("EffectiveCodec(no encoder, mp4) = %v, want CodecH264", got)
	}
	p := New(Encoder{Codec: CodecH265})
	if got := EffectiveCodec(p, FormatWebM); got != CodecH265 {
		t.Errorf("EffectiveCodec(explicit encoder) = %v, want CodecH265 (explicit wins)", got)
	}
}

func TestEffectiveContainerFromOutputExtension(t *testing.T) {
	if got := EffectiveContainer("OUT.webm", New()); got != FormatWebM {
		t.Errorf("EffectiveContainer(.webm) = %v, want FormatWebM", got)
	}
	if got := EffectiveContainer("OUT.mp4", New(Container{Format: FormatMKV})); got != FormatMKV {
		t.Errorf("EffectiveContainer with a Container step = %v, want FormatMKV (step wins)", got)
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
