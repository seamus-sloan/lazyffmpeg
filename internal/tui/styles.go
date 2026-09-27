package tui

import "charm.land/lipgloss/v2"

// menuWidth is the fixed width, in columns, of the MENU panel. The left
// column (preview + info + PIPELINE) takes the rest of the frame.
const menuWidth = 32

var (
	focusedHeadingStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle         = lipgloss.NewStyle().Bold(true)
	dimStyle            = lipgloss.NewStyle().Faint(true)
	errorStyle          = lipgloss.NewStyle().Bold(true)
)
