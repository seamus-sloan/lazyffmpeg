package runner

import (
	"strings"
	"testing"
)

func TestParseProgressEmitsOneSamplePerProgressLine(t *testing.T) {
	input := strings.Join([]string{
		"frame=30",
		"fps=30.00",
		"bitrate=1234.5kbits/s",
		"total_size=123456",
		"out_time_us=1000000",
		"out_time=00:00:01.000000",
		"speed=1.5x",
		"progress=continue",
		"frame=60",
		"out_time_us=2000000",
		"speed=2x",
		"progress=continue",
	}, "\n") + "\n"

	var samples []Sample
	ParseProgress(strings.NewReader(input), func(s Sample) {
		samples = append(samples, s)
	})

	if len(samples) != 2 {
		t.Fatalf("len(samples) = %d, want 2", len(samples))
	}
	if samples[0].OutTime != 1 || samples[0].Speed != 1.5 || samples[0].End {
		t.Errorf("samples[0] = %+v, want {OutTime:1 Speed:1.5 End:false}", samples[0])
	}
	if samples[1].OutTime != 2 || samples[1].Speed != 2 || samples[1].End {
		t.Errorf("samples[1] = %+v, want {OutTime:2 Speed:2 End:false}", samples[1])
	}
}

func TestParseProgressEnd(t *testing.T) {
	input := "out_time_us=5000000\nspeed=1x\nprogress=end\n"
	var samples []Sample
	ParseProgress(strings.NewReader(input), func(s Sample) {
		samples = append(samples, s)
	})
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if !samples[0].End {
		t.Error("End = false, want true")
	}
	if samples[0].OutTime != 5 {
		t.Errorf("OutTime = %v, want 5", samples[0].OutTime)
	}
}

func TestParseProgressNAValuesLeaveZero(t *testing.T) {
	input := "out_time_us=N/A\nspeed=N/A\nprogress=continue\n"
	var samples []Sample
	ParseProgress(strings.NewReader(input), func(s Sample) {
		samples = append(samples, s)
	})
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].OutTime != 0 || samples[0].Speed != 0 {
		t.Errorf("samples[0] = %+v, want zero OutTime/Speed", samples[0])
	}
}

func TestParseProgressIgnoresUnknownKeysAndBlankLines(t *testing.T) {
	input := "\nsome_unknown_key=xyz\n\nout_time_us=3000000\n\nprogress=continue\n\n"
	var samples []Sample
	ParseProgress(strings.NewReader(input), func(s Sample) {
		samples = append(samples, s)
	})
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].OutTime != 3 {
		t.Errorf("OutTime = %v, want 3", samples[0].OutTime)
	}
}
