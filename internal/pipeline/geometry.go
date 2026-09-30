package pipeline

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// ErrCropOutside is returned when a Crop step's box does not fit inside
// the frame it is applied to.
var ErrCropOutside = errors.New("crop is outside the frame")

// Crop cuts the frame down to a region. AspectW:AspectH set keeps the
// largest centered region of that shape; otherwise it keeps a Width x
// Height box, centered, or with its top-left corner at X,Y when Offset.
// Sizes are kept even, as Resolution's are, since yuv420p encoders reject
// odd dimensions.
type Crop struct {
	AspectW, AspectH int
	Width, Height    int
	X, Y             int
	Offset           bool
}

func (Crop) Kind() Kind { return KindCrop }

func (c Crop) Validate() error {
	aspect := c.AspectW > 0 || c.AspectH > 0
	box := c.Width > 0 || c.Height > 0
	switch {
	case aspect == box:
		return invalidf("crop needs either an aspect ratio or a width and height, not both or neither")
	case aspect && (c.AspectW < 1 || c.AspectH < 1 || c.AspectW > 100 || c.AspectH > 100):
		return invalidf("crop aspect %d:%d out of range [1,100]", c.AspectW, c.AspectH)
	case aspect && c.Offset:
		return invalidf("an aspect-ratio crop is always centered")
	case box && (c.Width < 2 || c.Height < 2 || c.Width > 16384 || c.Height > 16384):
		return invalidf("crop %d×%d out of range [2,16384]", c.Width, c.Height)
	case c.X < 0 || c.Y < 0:
		return invalidf("crop offset %d,%d must not be negative", c.X, c.Y)
	case !c.Offset && (c.X != 0 || c.Y != 0):
		return invalidf("a centered crop has no offset")
	}
	return nil
}

func (c Crop) Summary() string {
	switch {
	case c.AspectW > 0:
		return fmt.Sprintf("%d:%d", c.AspectW, c.AspectH)
	case c.Offset:
		return fmt.Sprintf("%d×%d at %d,%d", evenDown(c.Width), evenDown(c.Height), c.X, c.Y)
	}
	return fmt.Sprintf("%d×%d centered", evenDown(c.Width), evenDown(c.Height))
}

// VideoFilter crops with ffmpeg's crop filter, which centers the region
// unless given its position. An aspect crop's size is an expression of
// the frame's (its commas quoted from the filter chain's own).
func (c Crop) VideoFilter() string {
	switch {
	case c.AspectW > 0:
		return fmt.Sprintf("crop='trunc(min(iw,ih*%d/%d)/2)*2':'trunc(min(ih,iw*%d/%d)/2)*2'",
			c.AspectW, c.AspectH, c.AspectH, c.AspectW)
	case c.Offset:
		return fmt.Sprintf("crop=%d:%d:%d:%d", evenDown(c.Width), evenDown(c.Height), c.X, c.Y)
	}
	return fmt.Sprintf("crop=%d:%d", evenDown(c.Width), evenDown(c.Height))
}

func (Crop) AudioFilter() string { return "" }

// resize returns the frame size after the crop, or ErrCropOutside when a
// box crop does not fit a w x h frame.
func (c Crop) resize(w, h int) (int, int, error) {
	if c.AspectW > 0 {
		cw, ch := w, h
		if w*c.AspectH > h*c.AspectW {
			cw = h * c.AspectW / c.AspectH
		} else {
			ch = w * c.AspectH / c.AspectW
		}
		return evenDown(cw), evenDown(ch), nil
	}
	cw, ch := evenDown(c.Width), evenDown(c.Height)
	if c.X+cw > w || c.Y+ch > h {
		return 0, 0, fmt.Errorf("%w: %s does not fit the %d×%d frame", ErrCropOutside, c.Summary(), w, h)
	}
	return cw, ch, nil
}

// Flip mirrors the frame.
type Flip string

const (
	FlipNone       Flip = ""
	FlipHorizontal Flip = "h"
	FlipVertical   Flip = "v"
)

// Rotate turns the frame clockwise by Degrees (0, 90, 180 or 270), then
// mirrors it per Flip.
type Rotate struct {
	Degrees int
	Flip    Flip
}

func (Rotate) Kind() Kind { return KindRotate }

func (r Rotate) Validate() error {
	switch r.Degrees {
	case 0, 90, 180, 270:
	default:
		return invalidf("rotation %d° must be 0, 90, 180 or 270", r.Degrees)
	}
	switch r.Flip {
	case FlipNone, FlipHorizontal, FlipVertical:
	default:
		return invalidf("unknown flip %q", r.Flip)
	}
	if r.Degrees == 0 && r.Flip == FlipNone {
		return invalidf("rotate needs a rotation or a flip")
	}
	return nil
}

func (r Rotate) Summary() string {
	var parts []string
	switch r.Degrees {
	case 90:
		parts = append(parts, "90° clockwise")
	case 180:
		parts = append(parts, "180°")
	case 270:
		parts = append(parts, "90° counter-clockwise")
	}
	switch r.Flip {
	case FlipHorizontal:
		parts = append(parts, "flip horizontal")
	case FlipVertical:
		parts = append(parts, "flip vertical")
	}
	return strings.Join(parts, ", ")
}

func (r Rotate) VideoFilter() string {
	var f []string
	switch r.Degrees {
	case 90:
		f = append(f, "transpose=clock")
	case 180:
		f = append(f, "hflip", "vflip")
	case 270:
		f = append(f, "transpose=cclock")
	}
	switch r.Flip {
	case FlipHorizontal:
		f = append(f, "hflip")
	case FlipVertical:
		f = append(f, "vflip")
	}
	return strings.Join(f, ",")
}

func (Rotate) AudioFilter() string { return "" }

// resize returns the frame size after the rotation: a quarter turn swaps
// the sides.
func (r Rotate) resize(w, h int) (int, int, error) {
	if r.Degrees == 90 || r.Degrees == 270 {
		return h, w, nil
	}
	return w, h, nil
}

var cropBoxRe = regexp.MustCompile(`^(\d+)[x×X](\d+)(?:\+(\d+)\+(\d+))?$`)

// ParseCrop parses "W:H" (an aspect ratio, e.g. "16:9"), "WxH" (a centered
// box) or "WxH+X+Y" (a box with its top-left corner at X,Y).
func ParseCrop(s string) (Crop, error) {
	trimmed := strings.TrimSpace(s)
	var c Crop
	if a, b, ok := strings.Cut(trimmed, ":"); ok {
		w, err1 := strconv.Atoi(a)
		h, err2 := strconv.Atoi(b)
		if err1 != nil || err2 != nil {
			return Crop{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		c = Crop{AspectW: w, AspectH: h}
	} else {
		m := cropBoxRe.FindStringSubmatch(trimmed)
		if m == nil {
			return Crop{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		c.Width, _ = strconv.Atoi(m[1])
		c.Height, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			c.Offset = true
			c.X, _ = strconv.Atoi(m[3])
			c.Y, _ = strconv.Atoi(m[4])
		}
	}
	if err := c.Validate(); err != nil {
		return Crop{}, err
	}
	return c, nil
}

// ParseRotate parses a clockwise rotation in degrees (0, 90, 180 or 270;
// -90 for 270), a flip ("h" or "v"), or both separated by a space, as in
// "90 h".
func ParseRotate(s string) (Rotate, error) {
	var r Rotate
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 2 {
		return Rotate{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
	}
	for i, f := range fields {
		switch strings.ToLower(strings.TrimSuffix(f, "°")) {
		case "h":
			r.Flip = FlipHorizontal
			continue
		case "v":
			r.Flip = FlipVertical
			continue
		}
		deg, err := strconv.Atoi(strings.TrimSuffix(f, "°"))
		if err != nil || i != 0 {
			return Rotate{}, fmt.Errorf("%w: %q", ErrInvalidStep, s)
		}
		r.Degrees = ((deg % 360) + 360) % 360
	}
	if err := r.Validate(); err != nil {
		return Rotate{}, err
	}
	return r, nil
}

// checkFrameSize reports whether p's Crop steps each fit the frame they
// are applied to, when the input's size is known.
func checkFrameSize(info probe.Info, p Pipeline) error {
	if info.Video.Width <= 0 || info.Video.Height <= 0 {
		return nil
	}
	_, _, err := frameSize(info.Video.Width, info.Video.Height, p)
	return err
}

// frameResizer is a filter step that changes the frame's size.
type frameResizer interface {
	resize(w, h int) (int, int, error)
}

// frameSize walks p's filter steps from a w x h input frame and returns
// the frame's size after them, or the first Crop step that does not fit
// the frame it is applied to.
func frameSize(w, h int, p Pipeline) (int, int, error) {
	for _, s := range p.Steps() {
		if fr, ok := s.(frameResizer); ok {
			var err error
			if w, h, err = fr.resize(w, h); err != nil {
				return 0, 0, err
			}
		}
	}
	return w, h, nil
}
