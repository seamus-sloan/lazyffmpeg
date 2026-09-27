// Package preview renders one video frame, at a given input-timeline
// position, as terminal text: ffmpeg decodes and scales a single frame to
// PNG on stdout, piped (never through a shell) into chafa, which turns it
// into styled terminal symbols.
package preview

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// Request describes one frame to render.
type Request struct {
	Input      string
	Time       float64 // input-timeline seconds
	Filter     string  // spatial filter applied before the fit-scale; "" = original
	Cols, Rows int     // preview box size, in terminal cells
	Colors     string  // chafa --colors: "full", "256", "16", "none"
}

// FrameArgs is the ffmpeg argv that decodes Request's frame to PNG on
// stdout, scaled to fit a Cols*4 x Rows*8 pixel box.
func FrameArgs(r Request) []string {
	fit := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", r.Cols*4, r.Rows*8)
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

// Render decodes r's frame with ffmpeg and renders it with chafa, piping
// one process's stdout into the other's stdin directly (never through a
// shell). The result has at most r.Rows lines.
func Render(ctx context.Context, r Request) (string, error) {
	ffArgv := FrameArgs(r)
	ffCmd := exec.CommandContext(ctx, ffArgv[0], ffArgv[1:]...)
	var png, ffStderr bytes.Buffer
	ffCmd.Stdout = &png
	ffCmd.Stderr = &ffStderr
	if err := ffCmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(ffStderr.String()))
	}

	chArgv := ChafaArgs(r)
	chCmd := exec.CommandContext(ctx, chArgv[0], chArgv[1:]...)
	chCmd.Stdin = &png
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
