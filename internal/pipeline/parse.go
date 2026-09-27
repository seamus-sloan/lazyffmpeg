package pipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// ParseResolution parses "WxH" ("x", "X" or "×" as the separator, optional
// trailing "!" for Exact/stretch), "Wx", "xH" or "P%".
func ParseResolution(s string) (Resolution, error) {
	trimmed := strings.TrimSpace(s)
	if strings.HasSuffix(trimmed, "%") {
		pct, err := units.ParsePercent(trimmed)
		if err != nil {
			return Resolution{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		return Resolution{Percent: pct}, nil
	}

	body := trimmed
	exact := false
	if strings.HasSuffix(body, "!") {
		exact = true
		body = strings.TrimSuffix(body, "!")
	}

	norm := strings.NewReplacer("×", "x", "X", "x").Replace(body)
	parts := strings.SplitN(norm, "x", 2)
	if len(parts) != 2 {
		return Resolution{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}

	var w, h int
	if parts[0] != "" {
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return Resolution{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		w = v
	}
	if parts[1] != "" {
		v, err := strconv.Atoi(parts[1])
		if err != nil {
			return Resolution{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		h = v
	}

	r := Resolution{Width: w, Height: h, Exact: exact}
	if err := r.Validate(); err != nil {
		return Resolution{}, err
	}
	return r, nil
}

// ParseSpeed parses a bare number or a number with a trailing "x"/"X".
func ParseSpeed(s string) (Speed, error) {
	body := strings.TrimRight(strings.TrimSpace(s), "xX")
	v, err := strconv.ParseFloat(body, 64)
	if err != nil {
		return Speed{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	sp := Speed{Factor: v}
	if err := sp.Validate(); err != nil {
		return Speed{}, err
	}
	return sp, nil
}

// ParseTrim parses "start-end" where either side (never both) may be
// omitted: "5-" trims to the end, "-20" trims from the start.
func ParseTrim(s string) (Trim, error) {
	trimmed := strings.TrimSpace(s)
	idx := strings.Index(trimmed, "-")
	if idx < 0 {
		return Trim{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	startStr, endStr := trimmed[:idx], trimmed[idx+1:]
	if startStr == "" && endStr == "" {
		return Trim{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}

	var start, end float64
	if startStr != "" {
		v, err := units.ParseTime(startStr)
		if err != nil {
			return Trim{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		start = v
	}
	if endStr != "" {
		v, err := units.ParseTime(endStr)
		if err != nil {
			return Trim{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		end = v
	}

	t := Trim{Start: start, End: end}
	if err := t.Validate(); err != nil {
		return Trim{}, err
	}
	return t, nil
}

// ParseFPS parses a plain frame rate number.
func ParseFPS(s string) (FrameRate, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return FrameRate{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	f := FrameRate{FPS: v}
	if err := f.Validate(); err != nil {
		return FrameRate{}, err
	}
	return f, nil
}

// ParseCodec parses a short codec name (h264, h265, hevc, av1, vp9,
// h264-hw, h265-hw, copy) or an ffmpeg encoder name.
func ParseCodec(s string) (Encoder, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "h264", "libx264":
		return Encoder{Codec: CodecH264}, nil
	case "h265", "hevc", "libx265":
		return Encoder{Codec: CodecH265}, nil
	case "av1", "libsvtav1":
		return Encoder{Codec: CodecAV1}, nil
	case "vp9", "libvpx-vp9":
		return Encoder{Codec: CodecVP9}, nil
	case "h264-hw", "h264_videotoolbox":
		return Encoder{Codec: CodecH264HW}, nil
	case "h265-hw", "hevc_videotoolbox":
		return Encoder{Codec: CodecH265HW}, nil
	case "copy":
		return Encoder{Codec: CodecCopy}, nil
	}
	return Encoder{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
}

var crfRe = regexp.MustCompile(`(?i)^crf\s*(\d+)$`)

// ParseQuality parses a bare CRF number, "crf N"/"crfN", or a target size
// such as "20MB".
func ParseQuality(s string) (Quality, error) {
	trimmed := strings.TrimSpace(s)

	if m := crfRe.FindStringSubmatch(trimmed); m != nil {
		n, _ := strconv.Atoi(m[1])
		q := Quality{CRF: n}
		if err := q.Validate(); err != nil {
			return Quality{}, err
		}
		return q, nil
	}
	if n, err := strconv.Atoi(trimmed); err == nil {
		q := Quality{CRF: n}
		if err := q.Validate(); err != nil {
			return Quality{}, err
		}
		return q, nil
	}
	if size, err := units.ParseSize(trimmed); err == nil {
		return Quality{TargetBytes: size}, nil
	}
	return Quality{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
}

// ParseAudio parses "keep", "remove", "aac" (128k default), "aac:NNNk" or a
// bare "NNNk" bitrate.
func ParseAudio(s string) (Audio, error) {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)

	switch lower {
	case "keep":
		return Audio{Mode: AudioKeep}, nil
	case "remove":
		return Audio{Mode: AudioRemove}, nil
	case "aac":
		return Audio{Mode: AudioAAC, BitrateK: 128}, nil
	}
	if strings.HasPrefix(lower, "aac:") {
		k, err := units.ParseBitrateK(trimmed[len("aac:"):])
		if err != nil {
			return Audio{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		return Audio{Mode: AudioAAC, BitrateK: k}, nil
	}
	if k, err := units.ParseBitrateK(trimmed); err == nil {
		return Audio{Mode: AudioAAC, BitrateK: k}, nil
	}
	return Audio{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
}

// ParseContainer parses a container name, with or without a leading dot.
func ParseContainer(s string) (Container, error) {
	trimmed := strings.ToLower(strings.TrimSpace(s))
	trimmed = strings.TrimPrefix(trimmed, ".")
	switch trimmed {
	case "mp4":
		return Container{Format: FormatMP4}, nil
	case "mov":
		return Container{Format: FormatMOV}, nil
	case "mkv":
		return Container{Format: FormatMKV}, nil
	case "webm":
		return Container{Format: FormatWebM}, nil
	}
	return Container{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
}

// SplitArgs splits a raw-args string into argv elements, honoring single
// quotes (fully literal), double quotes (with \" and \\ escapes), and
// backslash escapes outside quotes. It never invokes a shell.
func SplitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	hasCur := false
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if hasCur {
			args = append(args, cur.String())
			cur.Reset()
			hasCur = false
		}
	}

	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			hasCur = true
			escaped = false
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(r)
			}
		case inDouble:
			switch r {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		default:
			switch r {
			case '\'':
				inSingle = true
				hasCur = true
			case '"':
				inDouble = true
				hasCur = true
			case '\\':
				escaped = true
			case ' ', '\t', '\n':
				flush()
			default:
				cur.WriteRune(r)
				hasCur = true
			}
		}
	}

	if escaped {
		return nil, fmt.Errorf("%w: unterminated escape in %q", ErrInvalidStep, s)
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("%w: unterminated quote in %q", ErrInvalidStep, s)
	}
	flush()

	if len(args) == 0 {
		return nil, fmt.Errorf("%w: no arguments in %q", ErrInvalidStep, s)
	}
	return args, nil
}
