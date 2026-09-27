// Package app wires the parsed command line to probing, compiling and
// running a pipeline (headless), or to launching the TUI. It must not
// import internal/tui.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"

	"github.com/seamus-sloan/lazyffmpeg/internal/cli"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// Version is the lazyff version string, set via
// -ldflags "-X github.com/seamus-sloan/lazyffmpeg/internal/app.Version=v1.2.3".
// The default "dev" falls back to the module version reported by
// debug.ReadBuildInfo, or literally "dev" when that is unavailable.
var Version = "dev"

var (
	// ErrSameAsInput is returned when the resolved output path is the
	// input file itself and --in-place was not given.
	ErrSameAsInput = errors.New("output is the input file; use --in-place to replace it")
	// ErrOutputExists is returned when the output already exists and
	// --force was not given.
	ErrOutputExists = errors.New("output exists")
)

// Session describes one file (or directory, for the picker) the TUI or a
// headless run operates on.
type Session struct {
	Input    string
	Info     probe.Info
	Pipeline pipeline.Pipeline
	Output   string // explicit -o; "" = derived from the pipeline
	InPlace  bool
	Force    bool
	Dir      string // absolute directory to browse when Input == ""
}

// OutputPath resolves the final output path for p: InPlace replaces the
// input; an explicit Output wins next; otherwise it is derived from the
// pipeline.
func (s Session) OutputPath(p pipeline.Pipeline) string {
	if s.InPlace {
		return s.Input
	}
	if s.Output != "" {
		return s.Output
	}
	return pipeline.DefaultOutputPath(s.Input, p)
}

// App is the headless/TUI entry point.
type App struct {
	Stdout, Stderr io.Writer
	StderrIsTTY    bool
	LaunchTUI      func(ctx context.Context, s Session) error
}

// Main runs lazyff for args (excluding the program name), returning the
// process exit code: 0 ok, 1 failure, 2 usage, 130 canceled.
func (a App) Main(ctx context.Context, args []string) int {
	cfg, err := cli.Parse(args)
	if err != nil {
		if errors.Is(err, cli.ErrUsage) {
			fmt.Fprintf(a.Stderr, "lazyff: %v\nRun 'lazyff --help' for usage.\n", err)
			return 2
		}
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	if cfg.ShowHelp {
		fmt.Fprint(a.Stdout, cli.Usage)
		return 0
	}
	if cfg.ShowVersion {
		fmt.Fprintf(a.Stdout, "lazyff %s\n", versionString())
		return 0
	}

	input := cfg.Input
	var dir string

	if input == "" {
		abs, err := filepath.Abs(".")
		if err != nil {
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
			return 1
		}
		dir = abs
	} else {
		fi, statErr := os.Stat(input)
		if statErr != nil {
			fmt.Fprintf(a.Stderr, "lazyff: %s: no such file or directory\n", input)
			return 1
		}
		if fi.IsDir() {
			abs, err := filepath.Abs(input)
			if err != nil {
				fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
				return 1
			}
			dir = abs
			input = ""
		}
	}

	if input == "" {
		if cfg.DryRun {
			fmt.Fprintf(a.Stderr, "lazyff: %s\nRun 'lazyff --help' for usage.\n", "--dry-run needs an input file")
			return 2
		}
		session := Session{
			Dir:      dir,
			Pipeline: cfg.Pipeline,
			Output:   cfg.Output,
			InPlace:  cfg.InPlace,
			Force:    cfg.Force,
		}
		return a.launchTUI(ctx, session)
	}

	if err := checkTools(); err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	info, err := probe.Run(ctx, input)
	if err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	session := Session{
		Input:    input,
		Info:     info,
		Pipeline: cfg.Pipeline,
		Output:   cfg.Output,
		InPlace:  cfg.InPlace,
		Force:    cfg.Force,
		Dir:      filepath.Dir(input),
	}
	outputPath := session.OutputPath(cfg.Pipeline)

	if cfg.DryRun {
		if !cfg.InPlace && sameAsInput(input, outputPath) {
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", ErrSameAsInput)
			return 1
		}
		argv, err := pipeline.Compile(info, cfg.Pipeline, pipeline.Options{Input: input, Output: outputPath})
		if err != nil {
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
			return 1
		}
		fmt.Fprintln(a.Stdout, pipeline.QuoteCommand(argv))
		return 0
	}

	if !cfg.HasSteps || cfg.TUI {
		return a.launchTUI(ctx, session)
	}

	if !cfg.InPlace && sameAsInput(input, outputPath) {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", ErrSameAsInput)
		return 1
	}

	if !cfg.InPlace && !cfg.Force {
		if _, statErr := os.Stat(outputPath); statErr == nil {
			fmt.Fprintf(a.Stderr, "lazyff: output exists: %s (use --force to overwrite)\n", outputPath)
			return 1
		}
	}

	argv, err := pipeline.Compile(info, cfg.Pipeline, pipeline.Options{Input: input, Output: outputPath})
	if err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	job := runner.Job{
		Argv:     argv,
		Output:   outputPath,
		Duration: pipeline.OutputDuration(info, cfg.Pipeline),
	}

	lastBucket := -1
	onProgress := func(p runner.Progress) {
		if a.StderrIsTTY {
			fmt.Fprintf(a.Stderr, "\rprogress: %.0f%%", p.Percent)
			return
		}
		bucket := int(p.Percent) / 10
		if bucket > lastBucket {
			lastBucket = bucket
			fmt.Fprintf(a.Stderr, "progress: %d%%\n", bucket*10)
		}
	}

	result, err := runner.Run(ctx, job, onProgress)
	if a.StderrIsTTY {
		fmt.Fprintln(a.Stderr)
	}
	if err != nil {
		if errors.Is(err, runner.ErrCanceled) {
			return 130
		}
		var exitErr *runner.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(a.Stderr, "lazyff: ffmpeg failed")
			for _, line := range exitErr.Tail {
				fmt.Fprintln(a.Stderr, line)
			}
			return 1
		}
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Stdout, result.Output)
	before := units.FormatSize(info.SizeBytes)
	after := units.FormatSize(result.Size)
	var pct float64
	if info.SizeBytes > 0 {
		pct = (float64(result.Size) - float64(info.SizeBytes)) / float64(info.SizeBytes) * 100
	}
	fmt.Fprintf(a.Stdout, "%s → %s (%+.0f%%)\n", before, after, pct)

	return 0
}

func (a App) launchTUI(ctx context.Context, s Session) int {
	if a.LaunchTUI == nil {
		fmt.Fprintln(a.Stderr, "lazyff: interactive mode not available")
		return 1
	}
	if err := a.LaunchTUI(ctx, s); err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}
	return 0
}

func versionString() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func sameAsInput(input, output string) bool {
	absIn, err1 := filepath.Abs(input)
	absOut, err2 := filepath.Abs(output)
	if err1 == nil && err2 == nil && filepath.Clean(absIn) == filepath.Clean(absOut) {
		return true
	}
	fiIn, errIn := os.Stat(input)
	fiOut, errOut := os.Stat(output)
	if errIn == nil && errOut == nil && os.SameFile(fiIn, fiOut) {
		return true
	}
	return false
}

func checkTools() error {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s not found on PATH (brew install ffmpeg)", name)
		}
	}
	return nil
}
