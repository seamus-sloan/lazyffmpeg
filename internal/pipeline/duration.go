package pipeline

import (
	"errors"
	"fmt"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// ErrTrimRange is returned when a Trim step's start is at or past the
// timeline duration at its position in the filter chain.
var ErrTrimRange = errors.New("trim is outside the clip")

// timelineState tracks the affine map from input-time to the current point
// in the filter chain as steps are walked in order: chain-local time =
// (input time - offset) / scale, valid while dur is the remaining duration
// at this point. Resolution and FrameRate never change it.
type timelineState struct {
	offset float64
	scale  float64
	dur    float64
}

// walkTimeline processes steps in order against an input of the given
// duration, returning the resulting timelineState. It returns
// ErrTrimRange if a Trim step's start is at or past the duration at its
// position in the chain.
func walkTimeline(steps []Step, duration float64) (timelineState, error) {
	st := timelineState{offset: 0, scale: 1, dur: duration}
	for _, s := range steps {
		switch v := s.(type) {
		case Speed:
			st.scale *= v.Factor
			st.dur /= v.Factor
		case Trim:
			if v.Start >= st.dur {
				return st, fmt.Errorf("%w: start %v at or past duration %v", ErrTrimRange, v.Start, st.dur)
			}
			end := v.End
			if end == 0 || end > st.dur {
				end = st.dur
			}
			st.offset += v.Start * st.scale
			st.dur = end - v.Start
		}
	}
	return st, nil
}

// OutputDuration returns the pipeline's output duration in seconds,
// accounting for Speed and Trim steps in order. It returns 0 for an
// invalid pipeline (see Validate).
func OutputDuration(info probe.Info, p Pipeline) float64 {
	st, err := walkTimeline(p.Steps(), info.Duration)
	if err != nil {
		return 0
	}
	return st.dur
}

// MapTime maps an input-timeline timestamp t through the filter steps
// [0, before) of p (indexes into p.Steps()): Speed divides by Factor; Trim
// subtracts Start and clamps to [0, End-Start] (no upper clamp when End is
// 0); other steps pass t through unchanged.
func MapTime(p Pipeline, before int, t float64) float64 {
	steps := p.Steps()
	if before > len(steps) {
		before = len(steps)
	}
	for i := 0; i < before; i++ {
		switch v := steps[i].(type) {
		case Speed:
			t /= v.Factor
		case Trim:
			t -= v.Start
			if t < 0 {
				t = 0
			}
			if v.End != 0 {
				if m := v.End - v.Start; t > m {
					t = m
				}
			}
		}
	}
	return t
}
