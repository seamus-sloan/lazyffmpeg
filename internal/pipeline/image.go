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
	// ErrConvertMismatch is returned when an image output's extension (from
	// -o or a File name step) names a different format than its Convert
	// step writes.
	ErrConvertMismatch = errors.New("output does not match the Convert format")
	// ErrConvertVideo is returned when a video's pipeline has a Convert
	// step.
	ErrConvertVideo = errors.New("Convert applies only to an image")
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
	return k.IsSpatial() || k == KindConvert || k == KindFilename || k == KindRawArgs
}

func (f ImageFormat) known() bool {
	switch f {
	case ImagePNG, ImageJPEG, ImageAVIF, ImageTIFF, ImageBMP:
		return true
	}
	return false
}

// Label is the format's name as shown to the user: "PNG", "JPEG", ...
func (f ImageFormat) Label() string {
	return strings.ToUpper(string(f))
}

// Ext is the extension an image of format f is written with.
func (f ImageFormat) Ext() string {
	switch f {
	case ImageJPEG:
		return ".jpg"
	case ImageTIFF:
		return ".tif"
	}
	return "." + string(f)
}

// Lossy reports whether f trades quality for size, so a Convert step's
// Quality applies to it.
func (f ImageFormat) Lossy() bool {
	return f == ImageJPEG || f == ImageAVIF
}

// Convert writes an image input as Format, at Quality percent (1-100,
// higher is better and bigger) for a lossy format; Quality 0 is the
// format's default (see imageEncoderArgs). It decides the output's
// extension, as a Container step does a video's.
type Convert struct {
	Format  ImageFormat
	Quality int
}

func (Convert) Kind() Kind { return KindConvert }

func (c Convert) Validate() error {
	switch {
	case !c.Format.known():
		return invalidf("unknown image format %q", c.Format)
	case c.Quality < 0 || c.Quality > 100:
		return invalidf("quality %d%% out of range [1,100]", c.Quality)
	case c.Quality > 0 && !c.Format.Lossy():
		return invalidf("%s is lossless, so it takes no quality", c.Format.Label())
	}
	return nil
}

func (c Convert) Summary() string {
	if c.Quality > 0 {
		return fmt.Sprintf("%s %d%%", c.Format.Label(), c.Quality)
	}
	return c.Format.Label()
}

// ParseConvert parses an image format ("png", "jpeg" or "jpg", "avif",
// "tiff" or "tif", "bmp", any case), optionally followed by a quality
// percent for a lossy one, as in "jpeg 85" or "avif 60%".
func ParseConvert(s string) (Convert, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 2 {
		return Convert{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	c := Convert{Format: ImageFormatOf("." + fields[0])}
	if c.Format == "" {
		return Convert{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	if len(fields) == 2 {
		q, err := strconv.Atoi(strings.TrimSuffix(fields[1], "%"))
		if err != nil || q < 1 {
			return Convert{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		c.Quality = q
	}
	if err := c.Validate(); err != nil {
		return Convert{}, err
	}
	return c, nil
}

// imageEncoderArgs are the ffmpeg arguments that encode one frame as f at
// quality percent (0 = the default). JPEG's quality maps onto mjpeg's
// -q:v scale (2 best, 31 worst), defaulting to 2, since mjpeg's own
// default is a low bitrate meant for video; AVIF's onto SVT-AV1's CRF (0
// best, 63 worst), defaulting to 30, which stays close to the source. The
// lossless formats take no quality.
func imageEncoderArgs(f ImageFormat, quality int) []string {
	switch f {
	case ImageJPEG:
		q := 2
		if quality > 0 {
			q = 2 + int(math.Round(float64(100-quality)*29/99))
		}
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(q)}
	case ImageAVIF:
		crf := 30
		if quality > 0 {
			crf = int(math.Round(float64(100-quality) * 63 / 100))
		}
		return []string{"-c:v", "libsvtav1", "-crf", strconv.Itoa(crf), "-pix_fmt", "yuv420p"}
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
	var quality int
	if s, ok := p.Find(KindConvert); ok {
		c := s.(Convert)
		if c.Format != format {
			return nil, fmt.Errorf("%w: %s is %s, Convert writes %s", ErrConvertMismatch,
				filepath.Base(opt.Output), format.Label(), c.Format.Label())
		}
		quality = c.Quality
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
	argv = append(argv, imageEncoderArgs(format, quality)...)
	argv = append(argv, "-update", "1")
	if raw, ok := p.Find(KindRawArgs); ok {
		argv = append(argv, raw.(RawArgs).Args...)
	}
	return append(argv, opt.Output), nil
}

// defaultImageExt is the extension a derived output name for image input
// gets: the Convert step's format's when p has one, else input's own when
// lazyff writes that format, else ".png".
func defaultImageExt(input string, p Pipeline) string {
	if c, ok := p.Find(KindConvert); ok {
		return c.(Convert).Format.Ext()
	}
	if ImageFormatOf(input) != "" {
		return filepath.Ext(input)
	}
	return ".png"
}
