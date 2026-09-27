package pipeline

import (
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

// Estimate predicts the output size for info/p/opt. Target-size pipelines
// return the target exactly; encoder copy scales the input bitrate by the
// output duration; everything else uses a per-codec/CRF bits-per-pixel
// heuristic. It returns the same error as Validate for an invalid pipeline.
func Estimate(info probe.Info, p Pipeline, opt Options) (SizeEstimate, error) {
	if err := Validate(info, p, opt); err != nil {
		return SizeEstimate{}, err
	}
	dur := OutputDuration(info, p)

	if q, ok := p.Find(KindQuality); ok {
		if qty := q.(Quality); qty.TargetBytes > 0 {
			return SizeEstimate{Bytes: qty.TargetBytes, Duration: dur, Kind: EstimateTarget}, nil
		}
	}

	container := effectiveContainer(opt.Output, p)
	codec := CodecH264
	if e, ok := p.Find(KindEncoder); ok {
		codec = e.(Encoder).Codec
	}
	if container == FormatWebM {
		if _, hasEncoder := p.Find(KindEncoder); !hasEncoder {
			codec = CodecVP9
		}
	}

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
// fitted (or exact/stretched) box for a Resolution step, the aspect-scaled
// even value for a single explicit side, or the input's own size when
// there is no Resolution step.
func OutputDimensions(info probe.Info, p Pipeline) (w, h int) {
	iw, ih := info.Video.Width, info.Video.Height
	r, ok := p.Find(KindResolution)
	if !ok {
		return iw, ih
	}
	res := r.(Resolution)
	switch {
	case res.Percent > 0:
		frac := res.Percent / 100
		return evenFloor(float64(iw) * frac), evenFloor(float64(ih) * frac)
	case res.Width > 0 && res.Height > 0:
		if res.Exact {
			return evenDown(res.Width), evenDown(res.Height)
		}
		scale := math.Min(float64(res.Width)/float64(iw), float64(res.Height)/float64(ih))
		return evenFloor(float64(iw) * scale), evenFloor(float64(ih) * scale)
	case res.Width > 0:
		return res.Width, evenRound(float64(ih) * float64(res.Width) / float64(iw))
	case res.Height > 0:
		return evenRound(float64(iw) * float64(res.Height) / float64(ih)), res.Height
	}
	return iw, ih
}

func evenFloor(x float64) int {
	return int(math.Floor(x/2)) * 2
}

func evenRound(x float64) int {
	return int(math.Round(x/2)) * 2
}
