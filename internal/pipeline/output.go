package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
)

// knownOutputExts are the container extensions lazyff can write into
// without an explicit Container step: DefaultOutputPath keeps the input's
// own extension only when it is one of these (case-insensitive); any other
// extension (including none at all) falls back to ".mp4", since ffmpeg
// infers its output muxer from the extension and most of the world's video
// extensions (.gif, .avi, .mkv-adjacent variants, ...) cannot hold an
// libx264/aac encode.
var knownOutputExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".webm": true,
}

// KnownContainerExt reports whether ext (with its leading dot, any case)
// is one of the container extensions lazyff can write without an explicit
// Container step: mp4, m4v, mov, mkv, webm.
func KnownContainerExt(ext string) bool {
	return knownOutputExts[strings.ToLower(ext)]
}

// DefaultOutputPath returns "<dir>/<stem> (edited).<ext>" for input, where
// <ext> is the container step's extension when p has one, else the input's
// own extension when it is a known container extension, else ".mp4".
func DefaultOutputPath(input string, p Pipeline) string {
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	if c, ok := p.Find(KindContainer); ok {
		ext = "." + string(c.(Container).Format)
	} else if !KnownContainerExt(ext) {
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
