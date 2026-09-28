# lazyff

A keyboard-driven terminal tool that chains video operations on **one**
input file and runs them as **one** ffmpeg invocation: resize, change
speed, trim, change frame rate, change encoder/quality/audio/container,
pass raw extra ffmpeg args, or rename the output file. Drive it from a
terminal UI with a live preview, or skip the UI entirely and run a chain
of flags headless.

## Install

```
go install github.com/seamus-sloan/lazyffmpeg/cmd/lazyff@latest
brew install ffmpeg chafa
```

`ffmpeg`/`ffprobe` are required; `chafa` is only needed for the terminal
preview (everything else still works without it).

## Usage

```
lazyff                                                 # file picker in the current directory
lazyff ~/Desktop                                       # file picker in that directory
lazyff <input>                                         # opens the TUI
lazyff <input> --width 1920 --height 1080 --speed 2    # step flags: run headless, no TUI
lazyff <input> --speed 2 --tui                         # open the TUI pre-seeded with those steps
lazyff <input> --speed 2 --dry-run                     # print the ffmpeg command, run nothing
lazyff <input> --speed 2 -o out.mp4                    # explicit output path
lazyff <input> --speed 2 --name demo                   # name the output (demo.<ext>, next to the input)
lazyff <input> --speed 2 --in-place                    # encode to a temp file, then replace the input
lazyff <input> --speed 2 --force                       # overwrite an existing output without asking
```

Step flags become pipeline steps **in the order given on the command
line**. `--width`/`--height` merge into one resolution step; either alone
keeps the aspect ratio.

If `<input>` is omitted, or is a directory, `lazyff` opens a file picker
there instead of the TUI editor. Picking a file from it probes the file
and moves straight to the editor, keeping any step flags and `-o`/
`--in-place`/`--force` already given.

### Step flags

| Flag | Value | Meaning |
|---|---|---|
| `--width N` | pixels | target width; fits the aspect ratio when paired with `--height` |
| `--height N` | pixels | target height |
| `--scale PCT` | e.g. `50%` | scale by percentage |
| `--stretch` | — | with both `--width`/`--height`, scale to exactly that size instead of fitting inside it |
| `--speed F` | e.g. `2`, `1.5` | playback speed factor |
| `--trim-start T` | seconds or `[hh:]mm:ss[.ms]` | trim start |
| `--trim-end T` | seconds or `[hh:]mm:ss[.ms]` | trim end |
| `--fps N` | number | output frame rate |
| `--encoder NAME` | `h264 h265 av1 vp9 h264-hw h265-hw copy` | video encoder |
| `--crf N` | number | constant rate factor (encoder-specific range) |
| `--target-size SIZE` | e.g. `20MB` | target output size (single-pass ABR) |
| `--audio MODE` | `keep remove aac[:BITRATE]` | audio handling |
| `--container FMT` | `mp4 mov mkv webm` | output container |
| `--name NAME` | file name | output file name, next to the input (see [Output file rules](#output-file-rules)) |
| `--args "..."` | raw args | extra ffmpeg arguments, inserted before the output path |

Other flags: `-o PATH`, `--in-place`, `--force`, `--tui`, `--dry-run`,
`--version`, `-h`/`--help`.

### Output file rules

Default output is `<name> (edited).<ext>` next to the input. `<ext>` is
the Container step's extension when set; else the input's own when it is
`mp4`, `m4v`, `mov`, `mkv` or `webm` (`m4v` only for H.264 or `copy`,
which is all it can hold); else `.mp4`. `-o` overrides it.

A File name step (`--name NAME`, or MENU's **File name**) renames the
output instead, keeping it next to the input (in `-o`'s directory when
the TUI was opened with `-o`). A name ending in `.mp4`, `.m4v`, `.mov`,
`.mkv` or `.webm` (any case) is used as is, and that extension picks the
container; it must agree with a Container step, and `.m4v` only holds
H.264 or `copy`. Any other name gets the default `<ext>` above appended:
`demo` → `demo.mov` for a `.mov` input, `my.clip` → `my.clip.mov`,
`demo.gif` → `demo.gif.mp4` for an `.mp4` input. The name is just a file
name: no `/` or `\`, no leading `.` (that would hide the file), no
control characters, at most 255 bytes. `--name` and `-o` both set the
output, so they cannot be combined.

`--in-place` encodes to a temp file in the same directory and atomically
replaces the input only after ffmpeg succeeds; it needs a format it can
write back (one of those extensions, with an encoder it can hold),
otherwise write a new file with `-o <name>.mp4`. It never renames:
`--name` with `--in-place` is a usage error, and the TUI refuses to run
an in-place session with a File name step (renaming in place would delete
the original). An output that resolves to the input itself is refused
unless `--in-place`. An existing output is never overwritten silently:
headless fails unless `--force`, the TUI asks first.

## Keybindings

### File picker

| Key | Action |
|---|---|
| `j`/`k`, `↑`/`↓` | move |
| `enter` | open the directory, or probe the file |
| `backspace`, `h`, `←` | go to the parent directory |
| `/` | filter by name (typing edits it, `esc` clears, `enter` accepts) |
| `q`, `ctrl+c` | quit |
| `?` | help |

### Main screen

| Key | Action |
|---|---|
| `tab` | switch focus between MENU and PIPELINE |
| `j`/`k`, `↑`/`↓` | move the cursor |
| `enter` (MENU) | open the selected kind's step modal |
| `e`/`enter` (PIPELINE) | edit the selected step |
| `x` (PIPELINE) | remove the selected step |
| `J`/`K` (PIPELINE) | move a filter step down/up |
| `u` | undo the last pipeline change |
| `c` | toggle the full command in the footer |
| `r`, or `enter` on MENU's Run | run |
| `q`, `ctrl+c` | quit |
| `?` | help |

### Preview

| Key | Action |
|---|---|
| `l`/`h` | seek ±1s |
| `L`/`H` | seek ±5s |
| `v` | toggle original/result (result applies the pipeline's spatial filters and limits the range to what a Trim step keeps) |
| `space` | play/pause |
| `i`/`o` | set the trim start/end at the current preview position |

### Step modal

`↑`/`↓` always move; `j`/`k` move only while a preset (not the Custom…
text box) is selected. `enter` confirms: a preset applies immediately, a
Custom… value is parsed and shown as an error under the box if invalid.
`esc` cancels with no change.

**File name** has no presets: its text box starts with the current output
file name (e.g. `clip (edited).mov`), ready to edit. Confirming adds the
step to PIPELINE and the footer's `→ <path>` follows it; `x` on the step
goes back to the default name, and `u` undoes either.

### Run

`y`/`n` answer an overwrite confirmation. While running, `esc` asks to
cancel; `q`/`ctrl+c` asks to cancel and quit. Any key dismisses the result
or error screen.

## How the pipeline compiles

Every run is **one** ffmpeg invocation, built from:

```
ffmpeg -hide_banner -nostdin -i <input>
  -map 0:V:0 [-map 0:a:0]
  [-vf <chain>] [-af <chain>]
  <video args> <audio args>
  [-movflags +faststart] [-f <muxer>] [raw args...]
  <output>
```

- The four filter steps (resolution, speed, trim, frame rate) join, in
  the order you arranged them, into one `-vf` chain and one `-af` chain.
  A resolution step with both sides set fits inside that box keeping
  aspect ratio; a trailing `!` (or `--stretch`) scales to it exactly.
- Default encoder (no Encoder step): `libx264`, CRF 23, `-preset medium`,
  `-pix_fmt yuv420p`. Other encoders: `libx265` (CRF 28, `-tag:v hvc1` on
  mp4/mov), `libsvtav1` (CRF 35), `libvpx-vp9` (CRF 33), `h264_videotoolbox`
  and `hevc_videotoolbox` (hardware, `-q:v` instead of CRF), or `copy`
  (no filters or quality setting allowed).
  A target size (`--target-size`, or the modal's Target presets) switches
  to single-pass ABR: `-b:v` from `(target bytes × 8 × 0.95 / output
  duration) − audio bitrate`, plus `-maxrate`/`-bufsize`.
- Audio defaults to a stream copy when the input's codec suits the
  container, else AAC 128k; `--audio remove` drops it, `--audio aac[:N]`
  re-encodes it explicitly.
- Container mp4/mov add `-movflags +faststart`. `webm` needs a vp9/av1
  video codec and opus/vorbis audio (AAC is rejected); with no Encoder
  step it defaults to vp9, and keep-mode audio re-encodes to opus unless
  the input is already opus/vorbis.
- Raw args (`--args`, quote-aware, no shell) are inserted right before
  the output path.
- A File name step adds no arguments: it only changes `<output>`.

The footer's `~<size>` estimate is a rough heuristic: output pixels × fps
× duration × a per-encoder/CRF bits-per-pixel factor, plus the audio
bitrate × duration. A target-size pipeline shows the target exactly; a
`copy` pipeline shows the input bitrate × output duration.
