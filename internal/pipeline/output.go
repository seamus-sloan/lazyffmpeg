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
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	if c, ok := p.Find(KindContainer); ok {
		ext = "." + string(c.(Container).Format)
	} else if !CanKeepExt(ext, p) {
		ext = ".mp4"
	}

	return filepath.Join(dir, stem+" (edited)"+ext)
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
