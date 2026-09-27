package pipeline

import (
	"errors"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func infoForEstimate(w, h int, fps float64, duration float64, bitRate int64) probe.Info {
	return probe.Info{
		Duration: duration,
		BitRate:  bitRate,
		Video:    probe.VideoStream{Codec: "h264", Width: w, Height: h, FPS: fps},
	}
}

func TestEstimateTarget(t *testing.T) {
	info := infoForEstimate(1920, 1080, 30, 10, 1_000_000)
	p := New(Quality{TargetBytes: 5_000_000})
	est, err := Estimate(info, p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if est.Kind != EstimateTarget {
		t.Errorf("Kind = %v, want EstimateTarget", est.Kind)
	}
	if est.Bytes != 5_000_000 {
		t.Errorf("Bytes = %v, want 5000000", est.Bytes)
	}
	if est.Duration != OutputDuration(info, p) {
		t.Errorf("Duration = %v, want %v", est.Duration, OutputDuration(info, p))
	}
}

func TestEstimateCopy(t *testing.T) {
	info := infoForEstimate(1920, 1080, 30, 10, 1_000_000)
	p := New(Encoder{Codec: CodecCopy})
	est, err := Estimate(info, p, Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if est.Kind != EstimateCopy {
		t.Errorf("Kind = %v, want EstimateCopy", est.Kind)
	}
	want := int64(1_000_000 * 10 / 8)
	if est.Bytes != want {
		t.Errorf("Bytes = %v, want %v", est.Bytes, want)
	}
}

func TestEstimateHeuristicLowerCRFDoubles(t *testing.T) {
	info := infoForEstimate(1920, 1080, 30, 10, 0)
	base, err := Estimate(info, New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(default): %v", err)
	}
	if base.Kind != EstimateHeuristic {
		t.Errorf("Kind = %v, want EstimateHeuristic", base.Kind)
	}
	if base.Bytes != 4_665_600 {
		t.Errorf("base Bytes = %v, want 4665600", base.Bytes)
	}

	lower, err := Estimate(info, New(Quality{CRF: 17}), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(crf17): %v", err)
	}
	if lower.Bytes != base.Bytes*2 {
		t.Errorf("lower CRF Bytes = %v, want %v (double)", lower.Bytes, base.Bytes*2)
	}
}

func TestEstimateHeuristicRemovingAudioLowers(t *testing.T) {
	info := infoAAC(10)
	withAudio, err := Estimate(info, New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(with audio): %v", err)
	}
	withoutAudio, err := Estimate(info, New(Audio{Mode: AudioRemove}), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(no audio): %v", err)
	}
	if withoutAudio.Bytes >= withAudio.Bytes {
		t.Errorf("removing audio should lower the estimate: with=%v without=%v", withAudio.Bytes, withoutAudio.Bytes)
	}
}

func TestEstimateHeuristicSpeed2Halves(t *testing.T) {
	info := infoForEstimate(1920, 1080, 30, 10, 0)
	base, err := Estimate(info, New(), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(base): %v", err)
	}
	sped, err := Estimate(info, New(Speed{Factor: 2}), Options{Input: "IN", Output: "OUT.mp4"})
	if err != nil {
		t.Fatalf("Estimate(speed2): %v", err)
	}
	if sped.Bytes != base.Bytes/2 {
		t.Errorf("2x speed Bytes = %v, want %v (half)", sped.Bytes, base.Bytes/2)
	}
}

func TestEstimateInvalidPipeline(t *testing.T) {
	info := infoForEstimate(1920, 1080, 30, 10, 0)
	_, err := Estimate(info, New(Speed{Factor: 1000}), Options{Input: "IN", Output: "OUT.mp4"})
	if !errors.Is(err, ErrInvalidStep) {
		t.Errorf("err = %v, want ErrInvalidStep", err)
	}
}

func TestOutputDimensionsExact(t *testing.T) {
	info := infoForEstimate(3652, 2560, 60, 33, 0)
	p := New(Resolution{Width: 1920, Height: 1080, Exact: true})
	w, h := OutputDimensions(info, p)
	if w != 1920 || h != 1080 {
		t.Errorf("OutputDimensions = %dx%d, want 1920x1080", w, h)
	}
}

func TestOutputDimensionsFitBox(t *testing.T) {
	info := infoForEstimate(3652, 2560, 60, 33, 0)
	p := New(Resolution{Width: 1920, Height: 1080})
	w, h := OutputDimensions(info, p)
	if w != 1540 || h != 1080 {
		t.Errorf("OutputDimensions = %dx%d, want 1540x1080", w, h)
	}
}

func TestOutputDimensionsFitRoundsToNearestEven(t *testing.T) {
	cases := []struct {
		iw, ih, boxW, boxH, wantW, wantH int
	}{
		{3652, 2560, 1280, 720, 1028, 720},
		{1000, 777, 640, 360, 464, 360},
		{641, 479, 320, 240, 320, 240},
	}
	for _, c := range cases {
		info := infoForEstimate(c.iw, c.ih, 30, 10, 0)
		p := New(Resolution{Width: c.boxW, Height: c.boxH})
		w, h := OutputDimensions(info, p)
		if w != c.wantW || h != c.wantH {
			t.Errorf("OutputDimensions(%dx%d into %dx%d) = %dx%d, want %dx%d",
				c.iw, c.ih, c.boxW, c.boxH, w, h, c.wantW, c.wantH)
		}
	}
}

func TestOutputDimensionsWidthOnly(t *testing.T) {
	info := infoForEstimate(3652, 2560, 60, 33, 0)
	p := New(Resolution{Width: 1280})
	w, h := OutputDimensions(info, p)
	if w != 1280 || h != 898 {
		t.Errorf("OutputDimensions = %dx%d, want 1280x898", w, h)
	}
}

func TestOutputDimensionsOddSingleSideIsEven(t *testing.T) {
	cases := []struct {
		res          Resolution
		wantW, wantH int
	}{
		{Resolution{Width: 161}, 160, 120},
		{Resolution{Height: 121}, 160, 120},
	}
	for _, c := range cases {
		info := infoForEstimate(320, 240, 30, 10, 0)
		w, h := OutputDimensions(info, New(c.res))
		if w != c.wantW || h != c.wantH {
			t.Errorf("OutputDimensions(%+v on 320x240) = %dx%d, want %dx%d", c.res, w, h, c.wantW, c.wantH)
		}
	}
}

func TestOutputDimensionsPercent(t *testing.T) {
	info := infoForEstimate(3652, 2560, 60, 33, 0)
	p := New(Resolution{Percent: 50})
	w, h := OutputDimensions(info, p)
	if w != 1826 || h != 1280 {
		t.Errorf("OutputDimensions = %dx%d, want 1826x1280", w, h)
	}
}

func TestOutputDimensionsNone(t *testing.T) {
	info := infoForEstimate(3652, 2560, 60, 33, 0)
	w, h := OutputDimensions(info, New())
	if w != 3652 || h != 2560 {
		t.Errorf("OutputDimensions = %dx%d, want 3652x2560 (input size)", w, h)
	}
}
