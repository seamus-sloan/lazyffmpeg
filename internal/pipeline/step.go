// Package pipeline models a chain of ffmpeg operations on one input file:
// an ordered set of filter steps plus a set of output settings, compiled
// into a single ffmpeg argv.
package pipeline

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// ErrInvalidStep is wrapped by every step validation error.
var ErrInvalidStep = errors.New("invalid step")

// Kind identifies a step's role in the pipeline.
type Kind int

const (
	KindResolution Kind = iota
	KindSpeed
	KindTrim
	KindFrameRate
	KindEncoder
	KindQuality
	KindAudio
	KindContainer
	KindRawArgs
)

// IsFilter reports whether k is one of the four ordered filter steps.
func (k Kind) IsFilter() bool {
	switch k {
	case KindResolution, KindSpeed, KindTrim, KindFrameRate:
		return true
	}
	return false
}

// Label is the human-readable name shown in the TUI's MENU and PIPELINE.
func (k Kind) Label() string {
	switch k {
	case KindResolution:
		return "Resolution"
	case KindSpeed:
		return "Speed"
	case KindTrim:
		return "Trim"
	case KindFrameRate:
		return "Frame rate"
	case KindEncoder:
		return "Encoder"
	case KindQuality:
		return "Quality"
	case KindAudio:
		return "Audio"
	case KindContainer:
		return "Container"
	case KindRawArgs:
		return "Raw args"
	}
	return ""
}

// Step is one entry in a Pipeline.
type Step interface {
	Kind() Kind
	Validate() error
	Summary() string
}

// FilterStep is a Step that contributes to the -vf/-af chains.
type FilterStep interface {
	Step
	VideoFilter() string
	AudioFilter() string
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidStep, fmt.Sprintf(format, args...))
}

// Resolution scales the video. Percent>0 scales by percentage; otherwise
// Width and/or Height give the target box. A 0 side keeps aspect (-2).
// When both sides are set and Exact is false, the video is fit inside the
// box keeping aspect (never squashed); Exact scales to exactly WxH.
type Resolution struct {
	Width, Height int
	Percent       float64
	Exact         bool
}

func (Resolution) Kind() Kind { return KindResolution }

func (r Resolution) Validate() error {
	if math.IsNaN(r.Percent) || math.IsInf(r.Percent, 0) {
		return invalidf("resolution percent must be a finite number")
	}
	hasPercent := r.Percent > 0
	hasSides := r.Width > 0 || r.Height > 0
	if hasPercent == hasSides {
		return invalidf("resolution needs either a percent or width/height, not both or neither")
	}
	if hasPercent && !(r.Percent > 0 && r.Percent <= 400) {
		return invalidf("resolution percent %v out of range (0,400]", r.Percent)
	}
	if r.Width > 0 && (r.Width < 2 || r.Width > 16384) {
		return invalidf("resolution width %d out of range [2,16384]", r.Width)
	}
	if r.Height > 0 && (r.Height < 2 || r.Height > 16384) {
		return invalidf("resolution height %d out of range [2,16384]", r.Height)
	}
	if r.Exact && (r.Width == 0 || r.Height == 0) {
		return invalidf("exact resolution needs both width and height")
	}
	return nil
}

// Normalize returns r with the sides the compiled filter actually uses: a
// single side, or both sides of an exact (stretched) size, rounded down to
// even, since yuv420p encoders such as libx264 reject odd dimensions (and
// ffmpeg's -2 keeps the other side even). A fit box is only a bound, the
// fitted output is kept even inside it, so its sides stay as given; so
// does a percent. Summary, VideoFilter and OutputDimensions all work from
// the normalized value, and ParseResolution returns it.
func (r Resolution) Normalize() Resolution {
	if r.Percent > 0 || (r.Width > 0 && r.Height > 0 && !r.Exact) {
		return r
	}
	r.Width, r.Height = evenDown(r.Width), evenDown(r.Height)
	return r
}

func (r Resolution) Summary() string {
	r = r.Normalize()
	switch {
	case r.Percent > 0:
		return units.FormatNumber(r.Percent) + "%"
	case r.Width > 0 && r.Height > 0:
		if r.Exact {
			return fmt.Sprintf("%d×%d (stretch)", r.Width, r.Height)
		}
		return fmt.Sprintf("fit %d×%d", r.Width, r.Height)
	case r.Width > 0:
		return fmt.Sprintf("%d×auto", r.Width)
	case r.Height > 0:
		return fmt.Sprintf("auto×%d", r.Height)
	}
	return ""
}

func (r Resolution) VideoFilter() string {
	r = r.Normalize()
	switch {
	case r.Percent > 0:
		frac := units.FormatNumber(r.Percent / 100)
		return fmt.Sprintf("scale=trunc(iw*%s/2)*2:-2", frac)
	case r.Width > 0 && r.Height > 0:
		if r.Exact {
			return fmt.Sprintf("scale=%d:%d", r.Width, r.Height)
		}
		return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2", r.Width, r.Height)
	case r.Width > 0:
		return fmt.Sprintf("scale=%d:-2", r.Width)
	case r.Height > 0:
		return fmt.Sprintf("scale=-2:%d", r.Height)
	}
	return ""
}

func (Resolution) AudioFilter() string { return "" }

// evenDown rounds n down to an even number (see Resolution.Normalize).
func evenDown(n int) int {
	return n - n%2
}

// Speed changes playback speed by Factor: video via setpts, audio via a
// chain of atempo stages each within [0.5,2.0].
type Speed struct {
	Factor float64
}

func (Speed) Kind() Kind { return KindSpeed }

func (s Speed) Validate() error {
	if !(s.Factor >= 0.01 && s.Factor <= 100) {
		return invalidf("speed factor %v out of range [0.01,100]", s.Factor)
	}
	return nil
}

func (s Speed) Summary() string {
	return units.FormatNumber(s.Factor) + "x"
}

func (s Speed) VideoFilter() string {
	if s.Factor == 1 {
		return ""
	}
	return fmt.Sprintf("setpts=PTS/%s", units.FormatNumber(s.Factor))
}

func (s Speed) AudioFilter() string {
	f := s.Factor
	var stages []string
	for f > 2 {
		stages = append(stages, formatAtempo(2.0))
		f /= 2
	}
	for f < 0.5 {
		stages = append(stages, formatAtempo(0.5))
		f /= 0.5
	}
	if f != 1 {
		stages = append(stages, formatAtempo(f))
	}
	return strings.Join(stages, ",")
}

func formatAtempo(f float64) string {
	s := units.FormatNumber(f)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return "atempo=" + s
}

// Trim cuts the timeline to [Start,End) at the position of this step in the
// chain. End == 0 means to the end of the input.
type Trim struct {
	Start, End float64
}

func (Trim) Kind() Kind { return KindTrim }

func (t Trim) Validate() error {
	if math.IsNaN(t.Start) || math.IsInf(t.Start, 0) {
		return invalidf("trim start must be a finite number")
	}
	if math.IsNaN(t.End) || math.IsInf(t.End, 0) {
		return invalidf("trim end must be a finite number")
	}
	if t.Start < 0 {
		return invalidf("trim start %v must be >= 0", t.Start)
	}
	if t.Start == 0 && t.End == 0 {
		return invalidf("trim needs a start or an end")
	}
	if t.End != 0 && t.End <= t.Start {
		return invalidf("trim end %v must be after start %v", t.End, t.Start)
	}
	return nil
}

func (t Trim) Summary() string {
	start := "start"
	if t.Start > 0 {
		start = units.FormatClock(t.Start)
	}
	end := "end"
	if t.End > 0 {
		end = units.FormatClock(t.End)
	}
	return start + " → " + end
}

func (t Trim) VideoFilter() string {
	s := fmt.Sprintf("trim=start=%s", units.FormatNumber(t.Start))
	if t.End != 0 {
		s += fmt.Sprintf(":end=%s", units.FormatNumber(t.End))
	}
	return s + ",setpts=PTS-STARTPTS"
}

func (t Trim) AudioFilter() string {
	s := fmt.Sprintf("atrim=start=%s", units.FormatNumber(t.Start))
	if t.End != 0 {
		s += fmt.Sprintf(":end=%s", units.FormatNumber(t.End))
	}
	return s + ",asetpts=PTS-STARTPTS"
}

// FrameRate changes the output frame rate.
type FrameRate struct {
	FPS float64
}

func (FrameRate) Kind() Kind { return KindFrameRate }

func (f FrameRate) Validate() error {
	if !(f.FPS > 0 && f.FPS <= 240) {
		return invalidf("frame rate %v out of range (0,240]", f.FPS)
	}
	return nil
}

func (f FrameRate) Summary() string {
	return units.FormatNumber(f.FPS) + " fps"
}

func (f FrameRate) VideoFilter() string {
	return fmt.Sprintf("fps=%s", units.FormatNumber(f.FPS))
}

func (FrameRate) AudioFilter() string { return "" }

// Codec names an ffmpeg video encoder (or "copy" for stream copy).
type Codec string

const (
	CodecH264   Codec = "libx264"
	CodecH265   Codec = "libx265"
	CodecAV1    Codec = "libsvtav1"
	CodecVP9    Codec = "libvpx-vp9"
	CodecH264HW Codec = "h264_videotoolbox"
	CodecH265HW Codec = "hevc_videotoolbox"
	CodecCopy   Codec = "copy"
)

// Label is the human-readable name shown in menus.
func (c Codec) Label() string {
	switch c {
	case CodecH264:
		return "H.264 (libx264)"
	case CodecH265:
		return "H.265 (libx265)"
	case CodecAV1:
		return "AV1 (libsvtav1)"
	case CodecVP9:
		return "VP9 (libvpx-vp9)"
	case CodecH264HW:
		return "H.264 hardware (h264_videotoolbox)"
	case CodecH265HW:
		return "H.265 hardware (hevc_videotoolbox)"
	case CodecCopy:
		return "Copy (no re-encode)"
	}
	return string(c)
}

func (c Codec) known() bool {
	switch c {
	case CodecH264, CodecH265, CodecAV1, CodecVP9, CodecH264HW, CodecH265HW, CodecCopy:
		return true
	}
	return false
}

// Encoder picks the output video codec.
type Encoder struct {
	Codec Codec
}

func (Encoder) Kind() Kind { return KindEncoder }

func (e Encoder) Validate() error {
	if !e.Codec.known() {
		return invalidf("unknown encoder %q", e.Codec)
	}
	return nil
}

func (e Encoder) Summary() string {
	return e.Codec.Label()
}

// Quality sets either a CRF value or a target output size in bytes.
// TargetBytes > 0 selects target-size mode.
type Quality struct {
	CRF         int
	TargetBytes int64
}

func (Quality) Kind() Kind { return KindQuality }

func (q Quality) Validate() error {
	if q.TargetBytes > 0 {
		return nil
	}
	if q.CRF < 0 || q.CRF > 63 {
		return invalidf("CRF %d out of range [0,63]", q.CRF)
	}
	return nil
}

func (q Quality) Summary() string {
	if q.TargetBytes > 0 {
		return fmt.Sprintf("target %.1f MB", float64(q.TargetBytes)/1e6)
	}
	return fmt.Sprintf("CRF %d", q.CRF)
}

// AudioMode selects how the audio stream is handled.
type AudioMode int

const (
	AudioKeep AudioMode = iota
	AudioRemove
	AudioAAC
)

// Audio sets the audio handling mode, with a bitrate for AudioAAC.
type Audio struct {
	Mode     AudioMode
	BitrateK int
}

func (Audio) Kind() Kind { return KindAudio }

func (a Audio) Validate() error {
	switch a.Mode {
	case AudioAAC:
		if a.BitrateK < 32 || a.BitrateK > 512 {
			return invalidf("aac bitrate %dk out of range [32,512]", a.BitrateK)
		}
	case AudioKeep, AudioRemove:
		if a.BitrateK != 0 {
			return invalidf("bitrate not allowed for this audio mode")
		}
	default:
		return invalidf("unknown audio mode")
	}
	return nil
}

func (a Audio) Summary() string {
	switch a.Mode {
	case AudioKeep:
		return "keep"
	case AudioRemove:
		return "remove"
	case AudioAAC:
		return fmt.Sprintf("AAC %dk", a.BitrateK)
	}
	return ""
}

// Format names an output container.
type Format string

const (
	FormatMP4  Format = "mp4"
	FormatMOV  Format = "mov"
	FormatMKV  Format = "mkv"
	FormatWebM Format = "webm"
)

func (f Format) known() bool {
	switch f {
	case FormatMP4, FormatMOV, FormatMKV, FormatWebM:
		return true
	}
	return false
}

// Container picks the output container format.
type Container struct {
	Format Format
}

func (Container) Kind() Kind { return KindContainer }

func (c Container) Validate() error {
	if !c.Format.known() {
		return invalidf("unknown container %q", c.Format)
	}
	return nil
}

func (c Container) Summary() string {
	return string(c.Format)
}

// RawArgs inserts extra ffmpeg arguments immediately before the output
// path. Text is the user-entered string; Args is its parsed form.
type RawArgs struct {
	Text string
	Args []string
}

func (RawArgs) Kind() Kind { return KindRawArgs }

func (r RawArgs) Validate() error {
	if len(r.Args) == 0 {
		return invalidf("raw args must not be empty")
	}
	return nil
}

func (r RawArgs) Summary() string {
	return r.Text
}
