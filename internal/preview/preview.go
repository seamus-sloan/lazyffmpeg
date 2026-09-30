// Package preview renders one video frame, at a given input-timeline
// position, for the terminal: ffmpeg decodes and scales a single frame to
// PNG on stdout, buffered in memory. That PNG is then either fed (never
// through a shell) to chafa's stdin, which turns it into styled terminal
// symbols, or sent to the terminal as-is as a kitty graphics image.
package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// Request describes one frame to render.
type Request struct {
	Input      string
	Time       float64 // input-timeline seconds
	Filter     string  // spatial filter applied before the fit-scale; "" = original
	Cols, Rows int     // preview box size, in terminal cells
	Colors     string  // chafa --colors: "full", "256", "16", "none"

	// ImageID, when nonzero, renders the frame as a kitty graphics image
	// transmitted under this id instead of as chafa symbols, fitted to
	// the preview box's size in pixels, PixelWidth x PixelHeight.
	ImageID                 int
	PixelWidth, PixelHeight int
}

// FrameArgs is the ffmpeg argv that decodes Request's frame to PNG on
// stdout. A symbols render scales it to fit a Cols*4 x Rows*8 pixel box;
// an image render scales it down to fit the box's PixelWidth x
// PixelHeight, but never up past the frame's own size (the terminal
// scales the image up to the box itself, so upscaling here would only
// send more bytes).
func FrameArgs(r Request) []string {
	fit := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", r.Cols*4, r.Rows*8)
	if r.ImageID != 0 {
		fit = fmt.Sprintf("scale='min(iw,%d)':'min(ih,%d)':force_original_aspect_ratio=decrease", r.PixelWidth, r.PixelHeight)
	}
	vf := fit
	if r.Filter != "" {
		vf = r.Filter + "," + fit
	}
	return []string{
		"ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", units.FormatNumber(r.Time),
		"-i", r.Input,
		"-frames:v", "1",
		"-vf", vf,
		"-f", "image2pipe",
		"-c:v", "png",
		"-",
	}
}

// ChafaArgs is the chafa argv that renders a PNG read from stdin as
// Cols x Rows terminal symbols.
func ChafaArgs(r Request) []string {
	return []string{
		"chafa",
		"--format", "symbols",
		"--size", fmt.Sprintf("%dx%d", r.Cols, r.Rows),
		"--animate", "off",
		"--polite", "on",
		"--colors", r.Colors,
		"-",
	}
}

// Available reports whether chafa is on PATH.
func Available() bool {
	_, err := exec.LookPath("chafa")
	return err == nil
}

// Render decodes r's frame with ffmpeg and buffers the PNG bytes it writes
// to stdout. For an image render (r.ImageID != 0) it returns KittyImage's
// sequence for them; otherwise it feeds that buffer to chafa's stdin
// (never through a shell) and returns chafa's symbols, at most r.Rows
// lines of them.
func Render(ctx context.Context, r Request) (string, error) {
	ffArgv := FrameArgs(r)
	ffCmd := exec.CommandContext(ctx, ffArgv[0], ffArgv[1:]...)
	var frame, ffStderr bytes.Buffer
	ffCmd.Stdout = &frame
	ffCmd.Stderr = &ffStderr
	if err := ffCmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(ffStderr.String()))
	}

	if r.ImageID != 0 {
		return KittyImage(r, frame.Bytes())
	}

	chArgv := ChafaArgs(r)
	chCmd := exec.CommandContext(ctx, chArgv[0], chArgv[1:]...)
	chCmd.Stdin = &frame
	var out, chStderr bytes.Buffer
	chCmd.Stdout = &out
	chCmd.Stderr = &chStderr
	if err := chCmd.Run(); err != nil {
		return "", fmt.Errorf("chafa: %v: %s", err, strings.TrimSpace(chStderr.String()))
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) > r.Rows {
		lines = lines[:r.Rows]
	}
	return strings.Join(lines, "\n"), nil
}

// kittyChunkSize is the most base64 payload one kitty graphics escape
// sequence may carry; a larger image is split across several.
const kittyChunkSize = 4096

// KittyImage returns the kitty graphics sequence that transmits frame (a
// PNG) as image r.ImageID and displays it at the cursor, without moving
// the cursor, over the cells FitCells gives it. Responses are suppressed,
// so the terminal never answers into the program's input.
func KittyImage(r Request, frame []byte) (string, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(frame))
	if err != nil {
		return "", fmt.Errorf("decoding frame: %v", err)
	}
	cols, rows := FitCells(cfg.Width, cfg.Height, r)

	payload := base64.StdEncoding.EncodeToString(frame)
	var b strings.Builder
	for start := 0; start < len(payload); start += kittyChunkSize {
		end := min(start+kittyChunkSize, len(payload))
		more := "m=0"
		if end < len(payload) {
			more = "m=1"
		}
		opts := []string{more}
		if start == 0 {
			opts = []string{
				"a=T", "f=100", "q=2", "C=1",
				"i=" + strconv.Itoa(r.ImageID),
				"c=" + strconv.Itoa(cols),
				"r=" + strconv.Itoa(rows),
				more,
			}
		}
		b.WriteString(ansi.KittyGraphics([]byte(payload[start:end]), opts...))
	}
	return b.String(), nil
}

// FitCells returns the cells a width x height pixel image covers when
// scaled to the largest size that fits r's box (r.Cols x r.Rows cells,
// r.PixelWidth x r.PixelHeight pixels) with its aspect ratio kept: the
// terminal stretches an image over exactly the cells it is given, so
// these must share the image's aspect ratio. Each is at least 1 and at
// most the box's.
func FitCells(width, height int, r Request) (cols, rows int) {
	scale := math.Min(float64(r.PixelWidth)/float64(width), float64(r.PixelHeight)/float64(height))
	cellW := float64(r.PixelWidth) / float64(r.Cols)
	cellH := float64(r.PixelHeight) / float64(r.Rows)
	cols = int(math.Round(float64(width) * scale / cellW))
	rows = int(math.Round(float64(height) * scale / cellH))
	return max(1, min(cols, r.Cols)), max(1, min(rows, r.Rows))
}

// DeleteKittyImage returns the kitty graphics sequence that removes image
// id from the screen and frees its data, suppressing any response.
func DeleteKittyImage(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", "i="+strconv.Itoa(id), "q=2")
}
