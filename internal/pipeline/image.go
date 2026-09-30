package pipeline

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// ImageFormat names a still-image file format lazyff writes.
type ImageFormat string

const (
	ImagePNG  ImageFormat = "png"
	ImageJPEG ImageFormat = "jpeg"
	ImageAVIF ImageFormat = "avif"
	ImageTIFF ImageFormat = "tiff"
	ImageBMP  ImageFormat = "bmp"
)

var (
	// ErrNotForImage is wrapped, after the step's label, when a pipeline
	// for an image input has a step that only applies to video, as in
	// "Speed does not apply to an image".
	ErrNotForImage = errors.New("does not apply to an image")
	// ErrImageFormat is returned when an image would be written with an
	// extension lazyff cannot write (WebP and HEIC can be read, not
	// written).
	ErrImageFormat = errors.New("cannot write this image format")
)

// ImageFormatOf returns the format an image named path is written in, by
// its extension (case-insensitively), or "" when lazyff does not write
// images with that extension.
func ImageFormatOf(path string) ImageFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return ImagePNG
	case ".jpg", ".jpeg":
		return ImageJPEG
	case ".avif":
		return ImageAVIF
	case ".tif", ".tiff":
		return ImageTIFF
	case ".bmp":
		return ImageBMP
	}
	return ""
}

// AppliesToImage reports whether steps of kind k can be applied to a
// still image: the spatial ones and the output's name and raw arguments,
// but nothing about time, audio or video encoding.
func (k Kind) AppliesToImage() bool {
	return k.IsSpatial() || k == KindFilename || k == KindRawArgs
}

// imageEncoderArgs are the ffmpeg arguments that encode one frame as f:
// JPEG at mjpeg's near-best quality (its default is a low bitrate meant
// for video), AVIF through SVT-AV1 at a CRF that keeps it close to the
// source, and the lossless formats as they are.
func imageEncoderArgs(f ImageFormat) []string {
	switch f {
	case ImageJPEG:
		return []string{"-c:v", "mjpeg", "-q:v", "2"}
	case ImageAVIF:
		return []string{"-c:v", "libsvtav1", "-crf", "30", "-pix_fmt", "yuv420p"}
	}
	return []string{"-c:v", string(f)}
}

// buildImage is Compile for a still-image input: the pipeline's spatial
// filters applied to its one frame, written as the format opt.Output's
// extension names. -update 1 tells ffmpeg's image muxer the output is one
// file, not a numbered sequence.
func buildImage(info probe.Info, p Pipeline, opt Options) ([]string, error) {
	steps := p.Steps()
	for _, s := range steps {
		if !s.Kind().AppliesToImage() {
			return nil, fmt.Errorf("%s %w", s.Kind().Label(), ErrNotForImage)
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}
	if err := checkFilename(p, opt.Input); err != nil {
		return nil, err
	}
	if err := checkFrameSize(info, p); err != nil {
		return nil, err
	}
	format := ImageFormatOf(opt.Output)
	if format == "" {
		return nil, fmt.Errorf("%w: %s (write .png, .jpg, .avif, .tif or .bmp)", ErrImageFormat, filepath.Base(opt.Output))
	}

	var vf []string
	for _, s := range steps {
		if fs, ok := s.(FilterStep); ok {
			if v := fs.VideoFilter(); v != "" {
				vf = append(vf, v)
			}
		}
	}

	argv := []string{"ffmpeg", "-hide_banner", "-nostdin", "-i", opt.Input, "-map", "0:V:0"}
	if len(vf) > 0 {
		argv = append(argv, "-vf", strings.Join(vf, ","))
	}
	argv = append(argv, "-frames:v", "1")
	argv = append(argv, imageEncoderArgs(format)...)
	argv = append(argv, "-update", "1")
	if raw, ok := p.Find(KindRawArgs); ok {
		argv = append(argv, raw.(RawArgs).Args...)
	}
	return append(argv, opt.Output), nil
}

// defaultImageExt is the extension a derived output name for image input
// gets: input's own when lazyff writes that format, else ".png".
func defaultImageExt(input string) string {
	if ImageFormatOf(input) != "" {
		return filepath.Ext(input)
	}
	return ".png"
}
