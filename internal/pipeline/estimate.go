package pipeline

import (
	"errors"
	"math"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// EstimateKind identifies how a SizeEstimate was computed.
type EstimateKind int

const (
	EstimateHeuristic EstimateKind = iota
	EstimateTarget
	EstimateCopy
)

// SizeEstimate is a rough, clearly-labelled prediction of the output size.
type SizeEstimate struct {
	Bytes    int64
	Duration float64
	Kind     EstimateKind
}

// ErrNoImageEstimate is Estimate's error for an image input: an image's
// size depends on its content far more than on its dimensions.
var ErrNoImageEstimate = errors.New("no size estimate for an image")

// Estimate predicts the output size for info/p/opt. Target-size pipelines
// return the target exactly; encoder copy scales the input bitrate by the
// output duration; everything else uses a per-codec/CRF bits-per-pixel
// heuristic. It returns the same error as Validate for an invalid pipeline.
func Estimate(info probe.Info, p Pipeline, opt Options) (SizeEstimate, error) {
	if err := Validate(info, p, opt); err != nil {
		return SizeEstimate{}, err
	}
	if probe.IsImage(opt.Input) {
		return SizeEstimate{}, ErrNoImageEstimate
	}
	dur := OutputDuration(info, p)

	if q, ok := p.Find(KindQuality); ok {
		if qty := q.(Quality); qty.TargetBytes > 0 {
			return SizeEstimate{Bytes: qty.TargetBytes, Duration: dur, Kind: EstimateTarget}, nil
		}
	}

	container := EffectiveContainer(opt.Output, p)
	codec := EffectiveCodec(p, container)

	if codec == CodecCopy {
		bytes := int64(float64(info.BitRate) * dur / 8)
		return SizeEstimate{Bytes: bytes, Duration: dur, Kind: EstimateCopy}, nil
	}

	w, h := OutputDimensions(info, p)
	fps := info.Video.FPS
	if fr, ok := p.Find(KindFrameRate); ok {
		fps = fr.(FrameRate).FPS
	}

	spec := codecSpecs[codec]
	crf := spec.defaultCRF
	if q, ok := p.Find(KindQuality); ok {
		crf = q.(Quality).CRF
	}

	videoBits := float64(w) * float64(h) * fps * dur * bppFor(codec) * math.Pow(2, float64(spec.defaultCRF-crf)/6)

	steps := p.Steps()
	hasAudioFilters := false
	for _, s := range steps {
		if fs, ok := s.(FilterStep); ok && fs.AudioFilter() != "" {
			hasAudioFilters = true
			break
		}
	}
	_, _, audioBps, _ := resolveAudio(info, p, container, hasAudioFilters)
	audioBits := float64(audioBps) * dur

	bytes := int64((videoBits + audioBits) / 8)
	return SizeEstimate{Bytes: bytes, Duration: dur, Kind: EstimateHeuristic}, nil
}

func bppFor(codec Codec) float64 {
	switch codec {
	case CodecH264:
		return 0.06
	case CodecH265:
		return 0.04
	case CodecAV1:
		return 0.035
	case CodecVP9:
		return 0.04
	case CodecH264HW:
		return 0.08
	case CodecH265HW:
		return 0.06
	}
	return 0.06
}

// OutputDimensions returns the compiled output's pixel dimensions: the
// input's size carried through each Resolution, Crop and Rotate step in
// order (see frameSize). A Crop that does not fit leaves the input's own
// size, since Validate rejects that pipeline anyway.
func OutputDimensions(info probe.Info, p Pipeline) (w, h int) {
	w, h, err := frameSize(info.Video.Width, info.Video.Height, p)
	if err != nil {
		return info.Video.Width, info.Video.Height
	}
	return w, h
}

// resize returns a w x h frame's size after the step: the fitted (or
// exact/stretched) box, a single explicit side rounded down to even with
// the other side aspect-scaled to even, or a percentage of both.
func (r Resolution) resize(w, h int) (int, int, error) {
	res := r.Normalize()
	switch {
	case res.Percent > 0:
		frac := res.Percent / 100
		return evenFloor(float64(w) * frac), evenFloor(float64(h) * frac), nil
	case res.Width > 0 && res.Height > 0:
		if res.Exact {
			return res.Width, res.Height, nil
		}
		scale := math.Min(float64(res.Width)/float64(w), float64(res.Height)/float64(h))
		return evenRound(float64(w) * scale), evenRound(float64(h) * scale), nil
	case res.Width > 0:
		return res.Width, evenRound(float64(h) * float64(res.Width) / float64(w)), nil
	case res.Height > 0:
		return evenRound(float64(w) * float64(res.Height) / float64(h)), res.Height, nil
	}
	return w, h, nil
}

func evenFloor(x float64) int {
	return int(math.Floor(x/2)) * 2
}

func evenRound(x float64) int {
	return int(math.Round(x/2)) * 2
}
