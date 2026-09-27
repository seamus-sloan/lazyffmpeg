// Package tui is lazyff's interactive terminal interface: a menu of step
// kinds, the pipeline being built, a live preview and a footer showing the
// compiled command, size estimate and output path.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
)

// focusArea names which panel has keyboard focus on the main screen.
type focusArea int

const (
	focusMenu focusArea = iota
	focusPipeline
)

// menuKinds lists the step kinds shown in MENU, in display order. The last
// selectable menu position (index len(menuKinds)) is the Run item.
var menuKinds = []pipeline.Kind{
	pipeline.KindResolution,
	pipeline.KindSpeed,
	pipeline.KindTrim,
	pipeline.KindFrameRate,
	pipeline.KindEncoder,
	pipeline.KindQuality,
	pipeline.KindAudio,
	pipeline.KindContainer,
	pipeline.KindRawArgs,
}

// menuRunIndex is the cursor position of the MENU's Run item.
var menuRunIndex = len(menuKinds)

func menuItemLabel(k pipeline.Kind) string {
	if k == pipeline.KindQuality {
		return "Quality / target size"
	}
	return k.Label()
}

// Model is the TUI's Bubble Tea model.
type Model struct {
	session  app.Session
	pipeline pipeline.Pipeline
	undo     []pipeline.Pipeline

	width, height int

	focus          focusArea
	menuCursor     int
	pipelineCursor int

	showFullCommand bool
	showHelp        bool
	quitting        bool
}

// Option configures a Model built by New.
type Option func(*Model)

// New builds a Model for session, applying any Options.
func New(s app.Session, opts ...Option) Model {
	m := Model{
		session:  s,
		pipeline: s.Pipeline,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Init starts the program; it currently issues no commands.
func (m Model) Init() tea.Cmd {
	return nil
}

// Pipeline returns the model's current pipeline value.
func (m Model) Pipeline() pipeline.Pipeline {
	return m.pipeline
}

// Update handles one message, per the Elm architecture.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "c":
		m.showFullCommand = !m.showFullCommand
		return m, nil
	case "tab":
		if m.focus == focusMenu {
			m.focus = focusPipeline
		} else {
			m.focus = focusMenu
		}
		return m, nil
	case "j", "down":
		m = m.moveCursor(1)
		return m, nil
	case "k", "up":
		m = m.moveCursor(-1)
		return m, nil
	}

	if m.focus == focusPipeline {
		switch key {
		case "x":
			m = m.removeStep()
			return m, nil
		case "J":
			m = m.moveStep(1)
			return m, nil
		case "K":
			m = m.moveStep(-1)
			return m, nil
		case "u":
			m = m.undoLast()
			return m, nil
		}
	}

	return m, nil
}

func (m Model) moveCursor(delta int) Model {
	if m.focus == focusMenu {
		m.menuCursor = clamp(m.menuCursor+delta, 0, menuRunIndex)
		return m
	}
	n := m.pipeline.Len()
	if n == 0 {
		m.pipelineCursor = 0
		return m
	}
	m.pipelineCursor = clamp(m.pipelineCursor+delta, 0, n-1)
	return m
}

func (m Model) pushUndo() Model {
	m.undo = append(append([]pipeline.Pipeline{}, m.undo...), m.pipeline)
	return m
}

func (m Model) removeStep() Model {
	steps := m.pipeline.Steps()
	if len(steps) == 0 {
		return m
	}
	i := m.pipelineCursor
	if i < 0 || i >= len(steps) {
		return m
	}
	m = m.pushUndo()
	m.pipeline = m.pipeline.Remove(i)
	n := m.pipeline.Len()
	if m.pipelineCursor >= n {
		m.pipelineCursor = n - 1
	}
	if m.pipelineCursor < 0 {
		m.pipelineCursor = 0
	}
	return m
}

func (m Model) moveStep(delta int) Model {
	steps := m.pipeline.Steps()
	i := m.pipelineCursor
	if i < 0 || i >= len(steps) || !steps[i].Kind().IsFilter() {
		return m
	}
	j := i + delta
	if j < 0 || j >= len(steps) || !steps[j].Kind().IsFilter() {
		return m
	}
	m = m.pushUndo()
	m.pipeline = m.pipeline.Move(i, delta)
	m.pipelineCursor = j
	return m
}

func (m Model) undoLast() Model {
	if len(m.undo) == 0 {
		return m
	}
	prev := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	m.pipeline = prev
	n := m.pipeline.Len()
	if m.pipelineCursor >= n {
		m.pipelineCursor = n - 1
	}
	if m.pipelineCursor < 0 {
		m.pipelineCursor = 0
	}
	return m
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Run starts the TUI program for session and blocks until it exits.
func Run(ctx context.Context, s app.Session) error {
	m := New(s)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
