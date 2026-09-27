package pipeline

import "github.com/seamus-sloan/lazyffmpeg/internal/probe"

// infoWithDuration returns a minimal probe.Info with the given duration and
// no audio, for tests that only exercise timeline math.
func infoWithDuration(d float64) probe.Info {
	return probe.Info{
		Duration: d,
		Video:    probe.VideoStream{Codec: "h264", Width: 1920, Height: 1080, FPS: 30},
	}
}
