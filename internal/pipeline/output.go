package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultOutputPath returns "<dir>/<stem> (edited).<ext>" for input, where
// <ext> is the container step's extension when p has one, else the input's
// own extension (".mp4" when the input has none).
func DefaultOutputPath(input string, p Pipeline) string {
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	if c, ok := p.Find(KindContainer); ok {
		ext = "." + string(c.(Container).Format)
	} else if ext == "" {
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
