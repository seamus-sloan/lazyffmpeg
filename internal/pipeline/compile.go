package pipeline

import (
	"path/filepath"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// Options are the input/output paths for one Compile call.
type Options struct {
	Input  string // passed verbatim after -i
	Output string // final output path; always the last argv element
}

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
	steps := p.Steps()
	for _, s := range steps {
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}
	if _, err := walkTimeline(steps, info.Duration); err != nil {
		return nil, err
	}

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

	container := effectiveContainer(opt.Output, p)

	mapAudio, audioArgs := resolveAudioKeep(info, container, hasAudioFilters)

	videoArgs := []string{"-c:v", "libx264", "-preset", "medium", "-crf", "23", "-pix_fmt", "yuv420p"}

	argv := []string{"ffmpeg", "-hide_banner", "-nostdin", "-i", opt.Input, "-map", "0:v:0"}
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
	argv = append(argv, opt.Output)

	return argv, nil
}

// effectiveContainer resolves the output's container: a Container step
// overrides the output path's extension.
func effectiveContainer(output string, p Pipeline) Format {
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

// resolveAudioKeep resolves the default (no Audio step) audio plan: stream
// copy when the input codec suits the container and no audio filters
// apply, else re-encode to AAC 128k. It returns whether to map/emit an
// audio stream at all, and the -c:a args to append.
func resolveAudioKeep(info probe.Info, container Format, hasAudioFilters bool) (mapAudio bool, args []string) {
	if info.Audio == nil {
		return false, nil
	}
	if hasAudioFilters {
		return true, []string{"-c:a", "aac", "-b:a", "128k"}
	}
	if copySuitable(info.Audio.Codec, container) {
		return true, []string{"-c:a", "copy"}
	}
	return true, []string{"-c:a", "aac", "-b:a", "128k"}
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
