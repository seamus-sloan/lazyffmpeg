package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrCanceled is returned by Run when ctx is canceled mid-run.
var ErrCanceled = errors.New("canceled")

// Job is one ffmpeg invocation to run.
type Job struct {
	Argv     []string // pipeline.Compile output; Argv[len-1] is the final output path
	Output   string   // final output path (== Argv[len-1]); the input path for --in-place
	Duration float64  // expected output duration (pipeline.OutputDuration) for percent
}

// Progress is one live update during a run.
type Progress struct {
	OutTime float64 // seconds of output written
	Speed   float64 // × realtime; 0 if unknown
	Percent float64 // 0-100, clamped; 100 on progress=end
	Elapsed time.Duration
	ETA     time.Duration // Elapsed*(100-Percent)/Percent; 0 while Percent == 0
}

// Result is a successful run's outcome.
type Result struct {
	Output  string
	Size    int64
	Elapsed time.Duration
}

// ExitError is returned when ffmpeg exits with a non-zero status.
type ExitError struct {
	Code int
	Tail []string // ffmpeg's last stderr lines, at most 20
}

func (e *ExitError) Error() string {
	last := ""
	if len(e.Tail) > 0 {
		last = e.Tail[len(e.Tail)-1]
	}
	return fmt.Sprintf("ffmpeg exited with code %d: %s", e.Code, last)
}

const tailLines = 20

// Run execs job.Argv[0] with -y, -progress, -nostats and the rest of
// job.Argv (minus the output path) against a temp file in the output's
// directory, calling onProgress as ffmpeg reports progress. On success the
// temp file is atomically renamed over job.Output; on failure or
// cancellation it is removed and job.Output (if it already existed) is
// left byte-for-byte unchanged.
func Run(ctx context.Context, job Job, onProgress func(Progress)) (Result, error) {
	if len(job.Argv) < 2 {
		return Result{}, fmt.Errorf("runner: job.Argv too short: %v", job.Argv)
	}

	dir := filepath.Dir(job.Output)
	ext := filepath.Ext(job.Output)
	tmp, err := os.CreateTemp(dir, ".lazyff-*"+ext)
	if err != nil {
		return Result{}, fmt.Errorf("runner: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // no-op once renamed away on success

	argv := make([]string, 0, len(job.Argv)+4)
	argv = append(argv, job.Argv[0], "-y", "-progress", "pipe:1", "-nostats")
	argv = append(argv, job.Argv[1:len(job.Argv)-1]...)
	argv = append(argv, tmpPath)

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("runner: stdout pipe: %w", err)
	}
	stderrBuf := newRingBuffer(tailLines)
	cmd.Stderr = stderrBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("runner: start ffmpeg: %w", err)
	}

	ParseProgress(stdout, func(s Sample) {
		elapsed := time.Since(start)
		pct := 0.0
		if job.Duration > 0 {
			pct = s.OutTime / job.Duration * 100
		}
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		if s.End {
			pct = 100
		}
		var eta time.Duration
		if pct > 0 {
			eta = time.Duration(float64(elapsed) * (100 - pct) / pct)
		}
		onProgress(Progress{OutTime: s.OutTime, Speed: s.Speed, Percent: pct, Elapsed: elapsed, ETA: eta})
	})

	waitErr := cmd.Wait()
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		return Result{}, ErrCanceled
	}

	if waitErr != nil {
		code := -1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		return Result{}, &ExitError{Code: code, Tail: stderrBuf.Lines()}
	}

	mode := os.FileMode(0644)
	if fi, statErr := os.Stat(job.Output); statErr == nil {
		mode = fi.Mode()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return Result{}, fmt.Errorf("runner: chmod output: %w", err)
	}
	if err := os.Rename(tmpPath, job.Output); err != nil {
		return Result{}, fmt.Errorf("runner: rename output: %w", err)
	}

	var size int64
	if fi, statErr := os.Stat(job.Output); statErr == nil {
		size = fi.Size()
	}

	return Result{Output: job.Output, Size: size, Elapsed: elapsed}, nil
}

// ringBuffer is an io.Writer that keeps the last max complete lines
// written to it, for capturing ffmpeg's stderr tail.
type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	cur   strings.Builder
	max   int
}

func newRingBuffer(max int) *ringBuffer {
	return &ringBuffer{max: max}
}

func (b *ringBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range p {
		if c == '\n' {
			b.push(b.cur.String())
			b.cur.Reset()
		} else {
			b.cur.WriteByte(c)
		}
	}
	return len(p), nil
}

func (b *ringBuffer) push(line string) {
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
}

// Lines returns the buffered lines, including any partial line not yet
// terminated by a newline.
func (b *ringBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := make([]string, len(b.lines))
	copy(lines, b.lines)
	if b.cur.Len() > 0 {
		lines = append(lines, b.cur.String())
		if len(lines) > b.max {
			lines = lines[len(lines)-b.max:]
		}
	}
	return lines
}
