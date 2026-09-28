// Package cli parses lazyff's command line, preserving the order in which
// step flags were given so they become pipeline steps in that order.
package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// ErrUsage is wrapped by every command-line usage error.
var ErrUsage = errors.New("usage")

// Config is the parsed command line.
type Config struct {
	Input       string
	Pipeline    pipeline.Pipeline
	HasSteps    bool
	Output      string // -o; "" when unset
	InPlace     bool
	Force       bool
	TUI         bool
	DryRun      bool
	ShowVersion bool
	ShowHelp    bool
}

// Usage is the full help text listing every flag.
const Usage = `Usage: lazyff [<input>] [flags]

If <input> is omitted, or is a directory, lazyff opens a file picker there.

Step flags (applied to the pipeline in the order given):
  --width N              target width in pixels
  --height N              target height in pixels
  --scale PCT             scale by percent, e.g. 50%
  --stretch               with --width and --height, scale to exactly
                          that size instead of fitting inside it
  --speed F               playback speed factor, e.g. 2, 1.5
  --trim-start T          trim start (seconds or [hh:]mm:ss[.ms])
  --trim-end T            trim end (seconds or [hh:]mm:ss[.ms])
  --fps N                 output frame rate
  --encoder NAME          video encoder (h264, h265, av1, vp9,
                          h264-hw, h265-hw, copy)
  --crf N                 constant rate factor
  --target-size SIZE      target output size, e.g. 20MB
  --audio MODE            keep, remove, aac[:BITRATE]
  --container FMT         mp4, mov, mkv, webm
  --name NAME             output file name, next to the input; a
                          .mp4/.mov/.mkv/.webm/.m4v ending sets the
                          container, else the default one is appended
  --args "..."            extra raw ffmpeg arguments

Other flags:
  -o PATH                 explicit output path
  --in-place              replace the input file
  --force                 overwrite an existing output
  --tui                   open the TUI (even with step flags)
  --dry-run               print the ffmpeg command, run nothing
  --version               print the version
  -h, --help              show this help
`

var valueFlagNames = map[string]bool{
	"--width": true, "--height": true, "--scale": true, "--speed": true,
	"--trim-start": true, "--trim-end": true, "--fps": true, "--encoder": true,
	"--crf": true, "--target-size": true, "--audio": true, "--container": true,
	"--name": true, "--args": true, "-o": true,
}

var boolFlagNames = map[string]bool{
	"-h": true, "--help": true, "--version": true, "--tui": true,
	"--dry-run": true, "--in-place": true, "--force": true, "--stretch": true,
}

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

type parser struct {
	cfg  Config
	pl   pipeline.Pipeline
	seen map[string]bool

	res      pipeline.Resolution
	hasScale bool
	hasSides bool

	trim    pipeline.Trim
	hasTrim bool

	hasCRF    bool
	hasTarget bool

	stretchSeen bool
}

// Parse parses args (excluding the program name) into a Config, preserving
// step-flag order.
func Parse(args []string) (Config, error) {
	pr := &parser{seen: map[string]bool{}}

	var positionals []string
	endOfFlags := false

	i := 0
	for i < len(args) {
		a := args[i]

		if endOfFlags {
			positionals = append(positionals, a)
			i++
			continue
		}
		if a == "--" {
			endOfFlags = true
			i++
			continue
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
			i++
			continue
		}

		name, inlineVal, hasInline := splitFlag(a)

		switch {
		case valueFlagNames[name]:
			if pr.seen[name] {
				return Config{}, usageErr("%s given more than once", name)
			}
			pr.seen[name] = true

			var val string
			if hasInline {
				val = inlineVal
			} else {
				if i+1 >= len(args) {
					return Config{}, usageErr("%s needs a value", name)
				}
				val = args[i+1]
				i++
			}
			if err := pr.applyValue(name, val); err != nil {
				return Config{}, err
			}
			i++

		case boolFlagNames[name]:
			if hasInline {
				return Config{}, usageErr("%s does not take a value", name)
			}
			if pr.seen[name] {
				return Config{}, usageErr("%s given more than once", name)
			}
			pr.seen[name] = true
			pr.applyBool(name)
			i++

		default:
			return Config{}, usageErr("unknown flag %s", name)
		}
	}

	if len(positionals) > 1 {
		return Config{}, usageErr("unexpected argument %q", positionals[1])
	}
	if len(positionals) == 1 {
		pr.cfg.Input = positionals[0]
	}

	if pr.hasTrim {
		if err := pr.trim.Validate(); err != nil {
			return Config{}, usageErr("invalid trim: %v", err)
		}
	}

	if pr.stretchSeen {
		if pr.res.Width == 0 || pr.res.Height == 0 {
			return Config{}, usageErr("--stretch needs both --width and --height")
		}
		pr.res.Exact = true
		pr.pl = pr.pl.Upsert(pr.res)
	}
	if pr.hasSides {
		// Only now is it known whether --width/--height is a single side
		// or a box: store the sides the encode will actually use.
		pr.res = pr.res.Normalize()
		pr.pl = pr.pl.Upsert(pr.res)
	}

	if pr.seen["-o"] && pr.cfg.InPlace {
		return Config{}, usageErr("-o cannot be combined with --in-place")
	}
	if pr.seen["--name"] && pr.seen["-o"] {
		return Config{}, usageErr("--name and -o both set the output; use one")
	}
	if pr.seen["--name"] && pr.cfg.InPlace {
		return Config{}, usageErr("--name cannot be combined with --in-place (renaming in place would delete the original)")
	}
	if pr.cfg.TUI && pr.cfg.DryRun {
		return Config{}, usageErr("--tui cannot be combined with --dry-run")
	}

	// Whether --in-place can keep the input's own container is checked in
	// the app package: Parse cannot tell a file input (whose extension
	// matters) from a directory input (browsed via the picker, whose name
	// just happens to have no extension of its own).

	pr.cfg.Pipeline = pr.pl
	return pr.cfg, nil
}

func splitFlag(a string) (name, val string, hasVal bool) {
	if idx := strings.Index(a, "="); idx >= 0 {
		return a[:idx], a[idx+1:], true
	}
	return a, "", false
}

func (pr *parser) applyBool(name string) {
	switch name {
	case "-h", "--help":
		pr.cfg.ShowHelp = true
	case "--version":
		pr.cfg.ShowVersion = true
	case "--tui":
		pr.cfg.TUI = true
	case "--dry-run":
		pr.cfg.DryRun = true
	case "--in-place":
		pr.cfg.InPlace = true
	case "--force":
		pr.cfg.Force = true
	case "--stretch":
		pr.stretchSeen = true
	}
}

func (pr *parser) applyValue(name, val string) error {
	switch name {
	case "--width":
		n, err := strconv.Atoi(val)
		if err != nil {
			return usageErr("invalid value for %s: %q", name, val)
		}
		if pr.hasScale {
			return usageErr("%s cannot be combined with --scale", name)
		}
		pr.hasSides = true
		pr.res.Width = n
		return pr.commitResolution()

	case "--height":
		n, err := strconv.Atoi(val)
		if err != nil {
			return usageErr("invalid value for %s: %q", name, val)
		}
		if pr.hasScale {
			return usageErr("%s cannot be combined with --scale", name)
		}
		pr.hasSides = true
		pr.res.Height = n
		return pr.commitResolution()

	case "--scale":
		if pr.hasSides {
			return usageErr("--scale cannot be combined with --width/--height")
		}
		pct, err := units.ParsePercent(val)
		if err != nil {
			return usageErr("invalid value for --scale: %q", val)
		}
		pr.hasScale = true
		pr.res.Percent = pct
		return pr.commitResolution()

	case "--speed":
		sp, err := pipeline.ParseSpeed(val)
		if err != nil {
			return usageErr("invalid value for --speed: %q", val)
		}
		pr.pl = pr.pl.Upsert(sp)
		pr.cfg.HasSteps = true

	case "--trim-start":
		t, err := units.ParseTime(val)
		if err != nil {
			return usageErr("invalid value for --trim-start: %q", val)
		}
		pr.trim.Start = t
		pr.commitTrim()

	case "--trim-end":
		t, err := units.ParseTime(val)
		if err != nil {
			return usageErr("invalid value for --trim-end: %q", val)
		}
		pr.trim.End = t
		pr.commitTrim()

	case "--fps":
		fr, err := pipeline.ParseFPS(val)
		if err != nil {
			return usageErr("invalid value for --fps: %q", val)
		}
		pr.pl = pr.pl.Upsert(fr)
		pr.cfg.HasSteps = true

	case "--encoder":
		e, err := pipeline.ParseCodec(val)
		if err != nil {
			return usageErr("invalid value for --encoder: %q", val)
		}
		pr.pl = pr.pl.Upsert(e)
		pr.cfg.HasSteps = true

	case "--crf":
		if pr.hasTarget {
			return usageErr("--crf cannot be combined with --target-size")
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return usageErr("invalid value for --crf: %q", val)
		}
		q := pipeline.Quality{CRF: n}
		if err := q.Validate(); err != nil {
			return usageErr("invalid value for --crf: %q", val)
		}
		pr.hasCRF = true
		pr.pl = pr.pl.Upsert(q)
		pr.cfg.HasSteps = true

	case "--target-size":
		if pr.hasCRF {
			return usageErr("--target-size cannot be combined with --crf")
		}
		sz, err := units.ParseSize(val)
		if err != nil {
			return usageErr("invalid value for --target-size: %q", val)
		}
		pr.hasTarget = true
		pr.pl = pr.pl.Upsert(pipeline.Quality{TargetBytes: sz})
		pr.cfg.HasSteps = true

	case "--audio":
		a, err := pipeline.ParseAudio(val)
		if err != nil {
			return usageErr("invalid value for --audio: %q", val)
		}
		pr.pl = pr.pl.Upsert(a)
		pr.cfg.HasSteps = true

	case "--container":
		c, err := pipeline.ParseContainer(val)
		if err != nil {
			return usageErr("invalid value for --container: %q", val)
		}
		pr.pl = pr.pl.Upsert(c)
		pr.cfg.HasSteps = true

	case "--name":
		f, err := pipeline.ParseFilename(val)
		if err != nil {
			return usageErr("invalid value for --name: %v", err)
		}
		pr.pl = pr.pl.Upsert(f)
		pr.cfg.HasSteps = true

	case "--args":
		parts, err := pipeline.SplitArgs(val)
		if err != nil {
			return usageErr("invalid value for --args: %q", val)
		}
		pr.pl = pr.pl.Upsert(pipeline.RawArgs{Text: val, Args: parts})
		pr.cfg.HasSteps = true

	case "-o":
		pr.cfg.Output = val
	}
	return nil
}

func (pr *parser) commitResolution() error {
	if err := pr.res.Validate(); err != nil {
		return usageErr("invalid resolution: %v", err)
	}
	pr.pl = pr.pl.Upsert(pr.res)
	pr.cfg.HasSteps = true
	return nil
}

// commitTrim records the merged --trim-start/--trim-end values at the
// position of the first trim flag. They are validated together once the
// whole command line is read (see Parse), because either flag alone can be
// invalid on its own ("--trim-start 0") yet fine once merged.
func (pr *parser) commitTrim() {
	pr.hasTrim = true
	pr.pl = pr.pl.Upsert(pr.trim)
	pr.cfg.HasSteps = true
}
