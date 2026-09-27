package runner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
	"github.com/seamus-sloan/lazyffmpeg/internal/testclip"
)

func noLazyffTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".lazyff-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("leftover temp files: %v", matches)
	}
}

func TestRunSuccess(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 2, Audio: true})
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}
	p := pipeline.New(pipeline.Speed{Factor: 2})
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	job := runner.Job{Argv: argv, Output: out, Duration: pipeline.OutputDuration(info, p)}

	var percents []float64
	result, err := runner.Run(context.Background(), job, func(p runner.Progress) {
		percents = append(percents, p.Percent)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(out); err != nil {
		t.Errorf("output does not exist: %v", err)
	}
	noLazyffTemps(t, dir)

	for i := 1; i < len(percents); i++ {
		if percents[i] < percents[i-1] {
			t.Errorf("Percent decreased: %v then %v", percents[i-1], percents[i])
		}
	}
	if len(percents) > 0 && percents[len(percents)-1] != 100 {
		t.Errorf("last Percent = %v, want 100", percents[len(percents)-1])
	}

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if fi.Mode().Perm() != 0644 {
		t.Errorf("mode = %v, want 0644 for a new file", fi.Mode().Perm())
	}
	if result.Size != fi.Size() {
		t.Errorf("Result.Size = %v, want %v", result.Size, fi.Size())
	}
	if result.Elapsed <= 0 {
		t.Errorf("Result.Elapsed = %v, want > 0", result.Elapsed)
	}
	if result.Output != out {
		t.Errorf("Result.Output = %q, want %q", result.Output, out)
	}
}

func TestRunReplacesOnlyAfterSuccess(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.mp4")

	original := []byte("not a real video, just a marker")
	if err := os.WriteFile(out, original, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}
	p := pipeline.New()
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	job := runner.Job{Argv: argv, Output: out, Duration: pipeline.OutputDuration(info, p)}

	if _, err := runner.Run(context.Background(), job, func(runner.Progress) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) == string(original) {
		t.Error("output was not replaced after a successful run")
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600 (the replaced file's original mode)", fi.Mode().Perm())
	}
	noLazyffTemps(t, dir)
}

func TestRunFailureLeavesExistingOutputUnchanged(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 1})
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.mp4")

	original := []byte("existing output content")
	if err := os.WriteFile(out, original, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// A malformed -vf value forces ffmpeg to fail.
	argv := []string{"ffmpeg", "-hide_banner", "-nostdin", "-i", in, "-map", "0:v:0",
		"-vf", "not_a_real_filter=xyz", "-c:v", "libx264", "-preset", "medium",
		"-crf", "23", "-pix_fmt", "yuv420p", "-an", out}
	job := runner.Job{Argv: argv, Output: out, Duration: 1}

	_, err := runner.Run(context.Background(), job, func(runner.Progress) {})
	if err == nil {
		t.Fatal("Run: expected an error, got nil")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v (%T), want *runner.ExitError", err, err)
	}
	if exitErr.Code == 0 {
		t.Errorf("ExitError.Code = 0, want non-zero")
	}
	if len(exitErr.Tail) == 0 {
		t.Error("ExitError.Tail is empty, want ffmpeg's stderr tail")
	}
	if len(exitErr.Tail) > 20 {
		t.Errorf("ExitError.Tail has %d lines, want <= 20", len(exitErr.Tail))
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(original) {
		t.Error("existing output was modified after a failed run")
	}
	noLazyffTemps(t, dir)
}

func TestRunCanceled(t *testing.T) {
	testclip.RequireTools(t, "ffmpeg", "ffprobe")
	in := testclip.Make(t, testclip.Spec{Width: 320, Height: 240, Seconds: 5, FPS: 30})
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.mp4")

	info, err := probe.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("probe.Run: %v", err)
	}
	p := pipeline.New()
	argv, err := pipeline.Compile(info, p, pipeline.Options{Input: in, Output: out})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	job := runner.Job{Argv: argv, Output: out, Duration: pipeline.OutputDuration(info, p)}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = runner.Run(ctx, job, func(runner.Progress) {})
	if !errors.Is(err, runner.ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("output should not exist after cancel")
	}
	noLazyffTemps(t, dir)
}
