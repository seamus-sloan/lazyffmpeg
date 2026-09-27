package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// menuWidth is the fixed width, in columns, of the MENU panel. The left
// column (preview + info + PIPELINE) takes the rest of the frame.
const menuWidth = 32

// The palette: mid-tone colours that read on dark and light terminal
// backgrounds alike. Bubble Tea downsamples them to whatever the terminal
// supports, and drops them entirely under NO_COLOR.
var (
	colorAccent = lipgloss.Color("#8B5CF6") // frame, focus, cursor, keys
	colorPink   = lipgloss.Color("#EC4899") // progress bar blend end
	colorCyan   = lipgloss.Color("#06B6D4") // times, video steps
	colorAmber  = lipgloss.Color("#F59E0B") // sizes, output settings, prompts
	colorGreen  = lipgloss.Color("#22C55E") // run, success, result preview
	colorRed    = lipgloss.Color("#EF4444") // errors
	colorBlue   = lipgloss.Color("#3B82F6") // directories
	colorGray   = lipgloss.Color("#6B7280") // inner box borders
)

var (
	frameBorderStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	boxBorderStyle      = lipgloss.NewStyle().Foreground(colorGray)
	brandStyle          = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	titleStyle          = lipgloss.NewStyle().Bold(true)
	focusedHeadingStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	headingStyle        = lipgloss.NewStyle().Bold(true).Faint(true)
	cursorStyle         = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle            = lipgloss.NewStyle().Faint(true)
	errorStyle          = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	successStyle        = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	promptStyle         = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	timeStyle           = lipgloss.NewStyle().Foreground(colorCyan)
	sizeStyle           = lipgloss.NewStyle().Foreground(colorAmber)
	videoStyle          = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	outputStyle         = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	dirStyle            = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	keyStyle            = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	commandStyle        = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	flagStyle           = lipgloss.NewStyle().Foreground(colorAccent)
)

// sep is the dimmed " · " separator used between fields on a line.
var sep = dimStyle.Render(" · ")

// cursorPrefix returns the two-column row prefix: a highlighted "> " on the
// cursor row, blank otherwise.
func cursorPrefix(on bool) string {
	if on {
		return cursorStyle.Render(">") + " "
	}
	return "  "
}

// hintLine renders "key desc · key desc …" with each key highlighted and
// each description dimmed; pairs alternate key, description.
func hintLine(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, sep)
}

// colorCommand highlights one line of a quoted ffmpeg command: the program
// name, and every token that starts with "-" (the options). A wrapped
// command is coloured line by line, so a token split across two lines is
// coloured by each half's own first character.
func colorCommand(line string) string {
	tokens := strings.Split(line, " ")
	for i, t := range tokens {
		switch {
		case t == "ffmpeg":
			tokens[i] = commandStyle.Render(t)
		case strings.HasPrefix(t, "-") && len(t) > 1:
			tokens[i] = flagStyle.Render(t)
		}
	}
	return strings.Join(tokens, " ")
}
