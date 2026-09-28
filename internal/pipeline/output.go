package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
)

// CanKeepExt reports whether an output named with extension ext (with its
// leading dot, any case), and no Container step, can hold p's encode: ext
// must name a container the compiler writes (see EffectiveContainer), and
// .m4v additionally only carries H.264 video or a stream copy, since its
// muxer rejects H.265, AV1 and VP9. ffmpeg infers the output muxer from
// the extension, and most other video extensions (.gif, .avi, ...) cannot
// hold these encodes at all.
func CanKeepExt(ext string, p Pipeline) bool {
	f := EffectiveContainer(ext, New())
	if f == "" {
		return false
	}
	if strings.EqualFold(ext, ".m4v") {
		switch EffectiveCodec(p, f) {
		case CodecH264, CodecH264HW, CodecCopy:
			return true
		}
		return false
	}
	return true
}

// DefaultOutputPath returns "<dir>/<stem> (edited).<ext>" for input, where
// <ext> is the container step's extension when p has one, else the input's
// own extension when CanKeepExt allows it, else ".mp4".
func DefaultOutputPath(input string, p Pipeline) string {
	base := filepath.Base(input)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(input), stem+" (edited)"+defaultExt(input, p))
}

// OutputPath resolves where p's encode of input is written, given explicit,
// the -o path ("" when unset). With a File name step, the file is its name
// in explicit's directory when explicit is set, else in input's; the name
// is used as is when it ends in a container extension lazyff writes (which
// then decides the container, as -o's extension does), and otherwise gets
// DefaultOutputPath's extension appended ("demo" → "demo.mov" for a .mov
// input). Without one, explicit wins, else DefaultOutputPath. Replacing
// the input in place is the caller's to decide first.
func OutputPath(input, explicit string, p Pipeline) string {
	if s, ok := p.Find(KindFilename); ok {
		dir := filepath.Dir(input)
		if explicit != "" {
			dir = filepath.Dir(explicit)
		}
		return filepath.Join(dir, namedFile(s.(Filename).Name, input, p))
	}
	if explicit != "" {
		return explicit
	}
	return DefaultOutputPath(input, p)
}

// namedFile is the file a File name step's name resolves to for input: the
// name itself when it ends in a container extension lazyff writes, else the
// name with defaultExt appended.
func namedFile(name, input string, p Pipeline) string {
	if EffectiveContainer(name, New()) != "" {
		return name
	}
	return name + defaultExt(input, p)
}

// defaultExt is the extension a derived output name gets: the container
// step's when p has one, else input's own when CanKeepExt allows it, else
// ".mp4".
func defaultExt(input string, p Pipeline) string {
	if c, ok := p.Find(KindContainer); ok {
		return "." + string(c.(Container).Format)
	}
	if ext := filepath.Ext(input); CanKeepExt(ext, p) {
		return ext
	}
	return ".mp4"
}

var bareArgRe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// QuoteCommand renders argv as a shell-quoted string for display (e.g.
// --dry-run output). It is never executed.
func QuoteCommand(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = quoteArg(a)
	}
	return strings.Join(parts, " ")
}

func quoteArg(a string) string {
	if a != "" && bareArgRe.MatchString(a) {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}
