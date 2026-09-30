package pipeline

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// Options are the input/output paths for one Compile call.
type Options struct {
	Input  string // passed verbatim after -i
	Output string // final output path; always the last argv element
}

var (
	ErrCopyWithFilters = errors.New("encoder copy cannot be combined with filter steps")
	ErrCopyWithQuality = errors.New("encoder copy cannot be combined with a quality setting")
	ErrCRFRange        = errors.New("CRF out of range for this encoder")
	ErrWebMVideo       = errors.New("webm needs vp9 or av1 video")
	ErrWebMAudio       = errors.New("webm cannot hold AAC audio")
	ErrUnknownDuration = errors.New("input duration is unknown")
	ErrTargetTooSmall  = errors.New("target size leaves under 50 kbps for video")

	// ErrFilenameContainer is returned when a File name step's extension
	// names a different container than the pipeline's Container step.
	ErrFilenameContainer = errors.New("file name does not match the container")
	// ErrFilenameM4V is returned when a File name step names an .m4v file
	// for video .m4v cannot hold (see CanKeepExt).
	ErrFilenameM4V = errors.New(".m4v holds only H.264 video or a stream copy")
	// ErrFilenameTooLong is returned when a File name step's name is
	// within the limit as typed but not once its extension is appended.
	ErrFilenameTooLong = errors.New("file name too long")
)

// Validate reports whether p can be compiled against info and opt, without
// building the argv.
func Validate(info probe.Info, p Pipeline, opt Options) error {
	_, err := build(info, p, opt)
	return err
}

// Compile turns info, p and opt into a single ffmpeg argv (Argv[0] ==
// "ffmpeg"). It calls Validate first.
func Compile(info probe.Info, p Pipeline, opt Options) ([]string, error) {
	return build(info, p, opt)
}

func build(info probe.Info, p Pipeline, opt Options) ([]string, error) {
	if probe.IsImage(opt.Input) {
		return buildImage(info, p, opt)
	}
	steps := p.Steps()
	for _, s := range steps {
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}
	if err := checkFilename(p, opt.Input); err != nil {
		return nil, err
	}
	if _, err := walkTimeline(steps, info.Duration); err != nil {
		return nil, err
	}
	if err := checkFrameSize(info, p); err != nil {
		return nil, err
	}
	outDur := OutputDuration(info, p)

	var vfParts, afParts []string
	for _, s := range steps {
		if fs, ok := s.(FilterStep); ok {
			if v := fs.VideoFilter(); v != "" {
				vfParts = append(vfParts, v)
			}
			if a := fs.AudioFilter(); a != "" {
				afParts = append(afParts, a)
			}
		}
	}
	vfChain := strings.Join(vfParts, ",")
	afChain := strings.Join(afParts, ",")
	hasAudioFilters := afChain != ""

	container := EffectiveContainer(opt.Output, p)
	codec := EffectiveCodec(p, container)

	if container == FormatWebM {
		if _, hasEncoder := p.Find(KindEncoder); hasEncoder {
			switch codec {
			case CodecVP9, CodecAV1:
				// ok
			case CodecCopy:
				if info.Video.Codec != "vp9" && info.Video.Codec != "av1" {
					return nil, ErrWebMVideo
				}
			default:
				return nil, ErrWebMVideo
			}
		}
	}

	qualityStep, hasQuality := p.Find(KindQuality)
	if codec == CodecCopy {
		if len(vfParts) > 0 || len(afParts) > 0 {
			return nil, ErrCopyWithFilters
		}
		if hasQuality {
			return nil, ErrCopyWithQuality
		}
	}

	mapAudio, audioArgs, audioBps, err := resolveAudio(info, p, container, hasAudioFilters)
	if err != nil {
		return nil, err
	}

	var videoArgs []string
	if codec == CodecCopy {
		videoArgs = []string{"-c:v", "copy"}
	} else {
		videoArgs, err = buildVideoArgs(codec, qualityStep, hasQuality, outDur, audioBps)
		if err != nil {
			return nil, err
		}
	}
	if needsHVC1Tag(codec, info, container) {
		videoArgs = append(videoArgs, "-tag:v", "hvc1")
	}

	argv := []string{"ffmpeg", "-hide_banner", "-nostdin", "-i", opt.Input, "-map", "0:V:0"}
	if mapAudio {
		argv = append(argv, "-map", "0:a:0")
	}
	if vfChain != "" {
		argv = append(argv, "-vf", vfChain)
	}
	if mapAudio && afChain != "" {
		argv = append(argv, "-af", afChain)
	}
	argv = append(argv, videoArgs...)
	argv = append(argv, audioArgs...)
	if container == FormatMP4 || container == FormatMOV {
		argv = append(argv, "-movflags", "+faststart")
	}
	if _, ok := p.Find(KindContainer); ok {
		argv = append(argv, "-f", muxerName(container))
	}
	if raw, ok := p.Find(KindRawArgs); ok {
		argv = append(argv, raw.(RawArgs).Args...)
	}
	argv = append(argv, opt.Output)

	return argv, nil
}

// checkFilename rejects a File name step whose extension the rest of p
// contradicts: one naming a container extension lazyff writes that is not
// the Container step's format, or an .m4v name for video .m4v cannot hold
// (see CanKeepExt). A name without such an extension (an image extension
// lazyff writes, for an image input) gets one appended (see OutputPath),
// so it cannot conflict, but the appended extension can take it over the
// file-name length limit.
func checkFilename(p Pipeline, input string) error {
	s, ok := p.Find(KindFilename)
	if !ok {
		return nil
	}
	name := s.(Filename).Name
	image := probe.IsImage(input)
	named := EffectiveContainer(name, New())
	if (image && ImageFormatOf(name) == "") || (!image && named == "") {
		if file := namedFile(name, input, p); len(file) > maxFilenameBytes {
			return fmt.Errorf("%w: %d bytes once %s is appended, over the %d-byte limit",
				ErrFilenameTooLong, len(file), strings.TrimPrefix(file, name), maxFilenameBytes)
		}
		return nil
	}
	if image {
		return nil
	}
	if c, ok := p.Find(KindContainer); ok && c.(Container).Format != named {
		return fmt.Errorf("%w: %s is %s, the container is %s", ErrFilenameContainer,
			name, strings.ToLower(filepath.Ext(name)), c.(Container).Format)
	}
	if !CanKeepExt(filepath.Ext(name), p) {
		return fmt.Errorf("%w (%s with %s); name it .mp4 instead", ErrFilenameM4V,
			name, EffectiveCodec(p, named))
	}
	return nil
}

// EffectiveContainer resolves the output's container: a Container step
// overrides the output path's extension.
func EffectiveContainer(output string, p Pipeline) Format {
	if c, ok := p.Find(KindContainer); ok {
		return c.(Container).Format
	}
	switch strings.ToLower(filepath.Ext(output)) {
	case ".mp4", ".m4v":
		return FormatMP4
	case ".mov":
		return FormatMOV
	case ".mkv":
		return FormatMKV
	case ".webm":
		return FormatWebM
	}
	return ""
}

// EffectiveCodec returns the video codec Compile would use for p against an
// output whose resolved container is out: the pipeline's Encoder step when
// it has one, VP9 for a webm output with none, else libx264. Callers such
// as the TUI's Quality modal use it to preselect a preset before Compile
// validates anything.
func EffectiveCodec(p Pipeline, out Format) Codec {
	if e, ok := p.Find(KindEncoder); ok {
		return e.(Encoder).Codec
	}
	if out == FormatWebM {
		return CodecVP9
	}
	return CodecH264
}

func muxerName(f Format) string {
	switch f {
	case FormatMP4:
		return "mp4"
	case FormatMOV:
		return "mov"
	case FormatMKV:
		return "matroska"
	case FormatWebM:
		return "webm"
	}
	return string(f)
}

func needsHVC1Tag(codec Codec, info probe.Info, container Format) bool {
	if container != FormatMP4 && container != FormatMOV {
		return false
	}
	if codec == CodecH265 || codec == CodecH265HW {
		return true
	}
	return codec == CodecCopy && info.Video.Codec == "hevc"
}

// resolveAudio decides the audio plan: whether to map/emit an audio
// stream at all, the -c:a (or -an) args to append, and the audio bitrate
// (bps) that a target-size video bitrate calculation should subtract.
func resolveAudio(info probe.Info, p Pipeline, container Format, hasAudioFilters bool) (mapAudio bool, args []string, bps int64, err error) {
	if info.Audio == nil {
		return false, nil, 0, nil
	}

	mode := AudioKeep
	bitrateK := 0
	if a, ok := p.Find(KindAudio); ok {
		aud := a.(Audio)
		mode = aud.Mode
		bitrateK = aud.BitrateK
	}

	if container == FormatWebM {
		switch mode {
		case AudioRemove:
			return false, []string{"-an"}, 0, nil
		case AudioAAC:
			return false, nil, 0, ErrWebMAudio
		default:
			if !hasAudioFilters && isOpusVorbis(info.Audio.Codec) {
				return true, []string{"-c:a", "copy"}, audioCopyBps(info), nil
			}
			return true, []string{"-c:a", "libopus", "-b:a", "128k"}, 128000, nil
		}
	}

	switch mode {
	case AudioRemove:
		return false, []string{"-an"}, 0, nil
	case AudioAAC:
		return true, []string{"-c:a", "aac", "-b:a", fmt.Sprintf("%dk", bitrateK)}, int64(bitrateK) * 1000, nil
	default:
		if hasAudioFilters {
			return true, []string{"-c:a", "aac", "-b:a", "128k"}, 128000, nil
		}
		if copySuitable(info.Audio.Codec, container) {
			return true, []string{"-c:a", "copy"}, audioCopyBps(info), nil
		}
		return true, []string{"-c:a", "aac", "-b:a", "128k"}, 128000, nil
	}
}

func isOpusVorbis(codec string) bool {
	return codec == "opus" || codec == "vorbis"
}

func audioCopyBps(info probe.Info) int64 {
	if info.Audio.BitRate != 0 {
		return info.Audio.BitRate
	}
	return 128000
}

func copySuitable(codec string, container Format) bool {
	if container == FormatMP4 || container == FormatMOV {
		switch codec {
		case "aac", "mp3", "alac", "ac3", "eac3":
			return true
		}
		return false
	}
	return true
}

type codecSpec struct {
	defaultCRF int
	maxCRF     int
	hardware   bool // uses -q:v instead of -crf
	vp9        bool // adds "-b:v 0" in CRF mode
	av1Target  bool // target mode emits only "-b:v Vk"
}

// DefaultCRF returns codec's default CRF value: the value Compile uses in
// the absence of a Quality step. Callers such as the TUI use it to
// preselect a Quality preset before Compile validates anything. Unknown
// codecs return 0.
func DefaultCRF(codec Codec) int {
	return codecSpecs[codec].defaultCRF
}

var codecSpecs = map[Codec]codecSpec{
	CodecH264:   {defaultCRF: 23, maxCRF: 51},
	CodecH265:   {defaultCRF: 28, maxCRF: 51},
	CodecAV1:    {defaultCRF: 35, maxCRF: 63, av1Target: true},
	CodecVP9:    {defaultCRF: 33, maxCRF: 63, vp9: true},
	CodecH264HW: {defaultCRF: 23, maxCRF: 51, hardware: true},
	CodecH265HW: {defaultCRF: 23, maxCRF: 51, hardware: true},
}

// buildVideoArgs builds the -c:v ... -pix_fmt yuv420p args for codec,
// either in CRF mode (default, or from a Quality step's CRF) or
// target-size mode (from a Quality step's TargetBytes), per the video-args
// table and target-size formula.
func buildVideoArgs(codec Codec, qualityStep Step, hasQuality bool, outDur float64, audioBps int64) ([]string, error) {
	spec := codecSpecs[codec]

	crf := spec.defaultCRF
	var targetBytes int64
	if hasQuality {
		q := qualityStep.(Quality)
		crf = q.CRF
		targetBytes = q.TargetBytes
	}

	var qualityArgs []string
	if targetBytes > 0 {
		if outDur <= 0 {
			return nil, ErrUnknownDuration
		}
		v := math.Floor((float64(targetBytes)*8*0.95/outDur - float64(audioBps)) / 1000)
		if v < 50 {
			return nil, ErrTargetTooSmall
		}
		vk := int64(v)
		if spec.av1Target {
			qualityArgs = []string{"-b:v", fmt.Sprintf("%dk", vk)}
		} else {
			qualityArgs = []string{"-b:v", fmt.Sprintf("%dk", vk), "-maxrate", fmt.Sprintf("%dk", vk), "-bufsize", fmt.Sprintf("%dk", vk*2)}
		}
	} else {
		if crf < 0 || crf > spec.maxCRF {
			return nil, ErrCRFRange
		}
		if spec.hardware {
			q := clampInt(int(math.Round(100-1.5*float64(crf))), 1, 100)
			qualityArgs = []string{"-q:v", strconv.Itoa(q)}
		} else if spec.vp9 {
			qualityArgs = []string{"-crf", strconv.Itoa(crf), "-b:v", "0"}
		} else {
			qualityArgs = []string{"-crf", strconv.Itoa(crf)}
		}
	}

	var args []string
	switch codec {
	case CodecH264:
		args = append(args, "-c:v", "libx264", "-preset", "medium")
	case CodecH265:
		args = append(args, "-c:v", "libx265", "-preset", "medium")
	case CodecAV1:
		args = append(args, "-c:v", "libsvtav1", "-preset", "8")
	case CodecVP9:
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "good", "-cpu-used", "4", "-row-mt", "1")
	case CodecH264HW:
		args = append(args, "-c:v", "h264_videotoolbox")
	case CodecH265HW:
		args = append(args, "-c:v", "hevc_videotoolbox")
	}
	args = append(args, qualityArgs...)
	args = append(args, "-pix_fmt", "yuv420p")
	return args, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
