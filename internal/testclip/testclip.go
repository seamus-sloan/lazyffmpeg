// Package testclip generates tiny lavfi-sourced video clips for integration
// tests, and skips tests when the required tools are absent. It is imported
// only from _test.go files.
package testclip

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Spec describes a test clip to generate.
type Spec struct {
	Name          string  // default "clip.mp4"
	Width, Height int     // default 320x240
	FPS           int     // default 30
	Seconds       float64 // default 2
	Audio         bool    // sine track: aac, or libopus when Name ends in .webm
}

// RequireTools skips the calling test when any of names is not on PATH.
func RequireTools(t testing.TB, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not found on PATH", name)
		}
	}
}

// Make generates a clip per s using ffmpeg's lavfi testsrc/sine sources and
// returns its absolute path inside t.TempDir().
func Make(t testing.TB, s Spec) string {
	t.Helper()
	RequireTools(t, "ffmpeg", "ffprobe")

	if s.Name == "" {
		s.Name = "clip.mp4"
	}
	if s.Width == 0 {
		s.Width = 320
	}
	if s.Height == 0 {
		s.Height = 240
	}
	if s.FPS == 0 {
		s.FPS = 30
	}
	if s.Seconds == 0 {
		s.Seconds = 2
	}

	dir := t.TempDir()
	path := filepath.Join(dir, s.Name)
	ext := strings.ToLower(filepath.Ext(s.Name))

	videoSrc := fmt.Sprintf("testsrc=size=%dx%d:rate=%d:duration=%g", s.Width, s.Height, s.FPS, s.Seconds)

	argv := []string{"-hide_banner", "-nostdin", "-f", "lavfi", "-i", videoSrc}
	if s.Audio {
		audioSrc := fmt.Sprintf("sine=frequency=440:duration=%g", s.Seconds)
		argv = append(argv, "-f", "lavfi", "-i", audioSrc)
	}

	videoCodec := "libx264"
	if ext == ".webm" {
		videoCodec = "libvpx-vp9"
	}
	argv = append(argv, "-c:v", videoCodec, "-pix_fmt", "yuv420p")

	if s.Audio {
		audioCodec := "aac"
		if ext == ".webm" {
			audioCodec = "libopus"
		}
		argv = append(argv, "-c:a", audioCodec, "-shortest")
	} else {
		argv = append(argv, "-an")
	}

	argv = append(argv, path)

	cmd := exec.Command("ffmpeg", argv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("testclip: ffmpeg failed: %v\n%s", err, out)
	}
	return path
}
