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
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/cli"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
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
	// ErrNameIsInput is wrapped, after the quoted name, when a File name
	// step resolves the output to the input file itself, as in
	// `file name "clip" is the input file; choose a different name`.
	// --in-place is no way out there: it never renames.
	ErrNameIsInput = errors.New("is the input file; choose a different name")
	// ErrOutputExists is returned when the output already exists and
	// --force was not given.
	ErrOutputExists = errors.New("output exists")
	// ErrInPlaceContainerMismatch is returned when --in-place is combined
	// with a Container step whose format differs from the input's own
	// extension: in-place never changes the container.
	ErrInPlaceContainerMismatch = errors.New("in-place cannot change the container")
	// ErrInPlaceUnsupportedExt is returned when --in-place is used on an
	// input whose format lazyff cannot write back: an extension it does
	// not write at all, or an .m4v input with an encoder .m4v cannot hold.
	ErrInPlaceUnsupportedExt = errors.New("in-place cannot write this format")
	// ErrInPlaceFormatMismatch is returned when --in-place is combined with
	// a Convert step to a different format than the input image's own.
	ErrInPlaceFormatMismatch = errors.New("in-place cannot change the image format")
	// ErrInPlaceRename is returned when --in-place is combined with a File
	// name step: renaming in place would delete the original.
	ErrInPlaceRename = errors.New("in-place cannot rename the file (that would delete the original)")
)

// CheckInPlace reports whether replacing input in place is safe for p: p
// must not have a File name step (in-place only ever writes back to
// input's own path, so a rename would mean deleting the original), a
// Container step's format must match input's own extension (in-place never
// changes the container), and when there is no Container step, input's own
// extension must be able to hold p's encode (pipeline.CanKeepExt); an image
// input's own format must be one lazyff writes (pipeline.ImageFormatOf). It is
// the single rule the headless path, --dry-run and the TUI's run
// confirmation all enforce, so an --in-place run never silently writes a
// different container over the original file. An unsupported format's
// error suggests writing a new .mp4 next to the input with -o instead.
func CheckInPlace(input string, p pipeline.Pipeline) error {
	if _, ok := p.Find(pipeline.KindFilename); ok {
		return ErrInPlaceRename
	}
	if probe.IsImage(input) {
		own := pipeline.ImageFormatOf(input)
		if own == "" {
			suggestion := pipeline.QuoteCommand([]string{strings.TrimSuffix(input, filepath.Ext(input)) + ".png"})
			return fmt.Errorf("%w (%s); write a new file with -o %s instead", ErrInPlaceUnsupportedExt,
				strings.ToLower(filepath.Ext(input)), suggestion)
		}
		if c, ok := p.Find(pipeline.KindConvert); ok && c.(pipeline.Convert).Format != own {
			return fmt.Errorf("%w (input is %s)", ErrInPlaceFormatMismatch, own.Label())
		}
		return nil
	}
	ext := filepath.Ext(input)
	inputExt := strings.ToLower(ext)
	if c, ok := p.Find(pipeline.KindContainer); ok {
		containerExt := "." + string(c.(pipeline.Container).Format)
		if !strings.EqualFold(containerExt, inputExt) {
			return fmt.Errorf("%w (input is %s)", ErrInPlaceContainerMismatch, inputExt)
		}
		return nil
	}
	if !pipeline.CanKeepExt(ext, p) {
		what := inputExt
		if what == "" {
			what = "no extension"
		}
		if e, ok := p.Find(pipeline.KindEncoder); ok {
			what += " with " + string(e.(pipeline.Encoder).Codec)
		}
		suggestion := pipeline.QuoteCommand([]string{strings.TrimSuffix(input, ext) + ".mp4"})
		return fmt.Errorf("%w (%s); write a new file with -o %s instead", ErrInPlaceUnsupportedExt, what, suggestion)
	}
	return nil
}

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
// input; a File name step names the file next (in Output's directory when
// Output is set); an explicit Output wins after that; otherwise it is
// derived from the pipeline (see pipeline.OutputPath). The headless path,
// --dry-run and the TUI all resolve the output here.
func (s Session) OutputPath(p pipeline.Pipeline) string {
	if s.InPlace {
		return s.Input
	}
	return pipeline.OutputPath(s.Input, s.Output, p)
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
		if cfg.DryRun || cfg.Symbols {
			flag := "--dry-run"
			if cfg.Symbols {
				flag = "--symbols"
			}
			fmt.Fprintf(a.Stderr, "lazyff: %s needs an input file\nRun 'lazyff --help' for usage.\n", flag)
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

	if cfg.Symbols {
		return a.printSymbols(ctx, input, info, cfg)
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

	if !cfg.DryRun && (!cfg.HasSteps || cfg.TUI) {
		return a.launchTUI(ctx, session)
	}

	if cfg.InPlace {
		if err := CheckInPlace(input, cfg.Pipeline); err != nil {
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
			return 1
		}
	}

	if !cfg.InPlace {
		if err := CheckNotInput(input, outputPath, cfg.Pipeline); err != nil {
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
			return 1
		}
	}

	if !cfg.DryRun && !cfg.InPlace && !cfg.Force {
		if _, statErr := os.Stat(outputPath); statErr == nil {
			err := fmt.Errorf("%w: %s (use --force to overwrite)", ErrOutputExists, outputPath)
			fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
			return 1
		}
	}

	argv, err := pipeline.Compile(info, cfg.Pipeline, pipeline.Options{Input: input, Output: outputPath})
	if err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}

	if cfg.DryRun {
		fmt.Fprintln(a.Stdout, pipeline.QuoteCommand(argv))
		return 0
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
	fmt.Fprintln(a.Stdout, units.FormatSizeChange(info.SizeBytes, result.Size))

	return 0
}

// printSymbols prints input's frame at cfg.SymbolsTime as plain-text
// symbols, cfg.SymbolsWidth columns wide (see preview.Request.Plain).
func (a App) printSymbols(ctx context.Context, input string, info probe.Info, cfg cli.Config) int {
	if !preview.Available() {
		fmt.Fprintln(a.Stderr, "lazyff: chafa not found on PATH (brew install chafa)")
		return 1
	}
	if info.Duration > 0 && cfg.SymbolsTime >= info.Duration {
		fmt.Fprintf(a.Stderr, "lazyff: --symbols %s is past the end of the input (%s)\n",
			units.FormatClock(cfg.SymbolsTime), units.FormatClock(info.Duration))
		return 1
	}
	t := cfg.SymbolsTime
	if probe.IsImage(input) {
		t = 0 // an image has one frame, whatever time was asked for
	}
	text, err := preview.Render(ctx, preview.Request{
		Input: input,
		Time:  t,
		Cols:  cfg.SymbolsWidth,
		Plain: true,
	})
	if err != nil {
		fmt.Fprintf(a.Stderr, "lazyff: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Stdout, text)
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

// CheckNotInput returns nil unless output resolves to input itself (see
// SameAsInput). Then it returns ErrNameIsInput naming p's File name step
// when p has one (that name produced the output), else ErrSameAsInput.
// The headless path, --dry-run and the TUI's run path all apply it
// whenever the input is not being replaced in place.
func CheckNotInput(input, output string, p pipeline.Pipeline) error {
	if !SameAsInput(input, output) {
		return nil
	}
	if s, ok := p.Find(pipeline.KindFilename); ok {
		return fmt.Errorf("file name %q %w", s.(pipeline.Filename).Name, ErrNameIsInput)
	}
	return ErrSameAsInput
}

// SameAsInput reports whether output resolves to the same file as input:
// by cleaned absolute path, or (when both exist) by os.SameFile. The TUI's
// run path uses this to apply the same output-safety rule as the headless
// path (see ErrSameAsInput).
func SameAsInput(input, output string) bool {
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
