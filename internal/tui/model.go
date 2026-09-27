// Package tui is lazyff's interactive terminal interface: a menu of step
// kinds, the pipeline being built, a live preview and a footer showing the
// compiled command, size estimate and output path.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
)

// screenMode names which screen the program is showing.
type screenMode int

const (
	modeMain screenMode = iota
	modePicker
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
	ctx context.Context

	mode   screenMode
	picker pickerState
	modal  *modalState
	run    runState

	preview           previewState
	renderFn          RenderFunc
	rendererAvailable bool
	colorProfile      string

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

	listFn  ListFunc
	probeFn ProbeFunc
	runFn   RunFunc
}

// Option configures a Model built by New.
type Option func(*Model)

// WithLister overrides the function used to list a directory (Task 10b).
func WithLister(fn ListFunc) Option {
	return func(m *Model) { m.listFn = fn }
}

// WithProber overrides the function used to probe a chosen file (Task 10b).
func WithProber(fn ProbeFunc) Option {
	return func(m *Model) { m.probeFn = fn }
}

// withContext threads the outer run context through to async commands. It
// is unexported: only Run sets it, tests use the zero value's background
// context.
func withContext(ctx context.Context) Option {
	return func(m *Model) { m.ctx = ctx }
}

// New builds a Model for session, applying any Options. When session.Input
// is empty, the model starts in the file picker, browsing session.Dir.
func New(s app.Session, opts ...Option) Model {
	m := Model{
		ctx:      context.Background(),
		session:  s,
		pipeline: s.Pipeline,
		listFn:   defaultList,
		probeFn:  probe.Run,
		runFn:    runner.Run,

		renderFn:          preview.Render,
		rendererAvailable: preview.Available(),
		colorProfile:      "256",
	}
	if s.Input == "" {
		m.mode = modePicker
		m.picker.dir = s.Dir
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Init starts the program: in picker mode it kicks off the initial
// directory listing.
func (m Model) Init() tea.Cmd {
	if m.mode == modePicker {
		return m.listCmd()
	}
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
		if m.mode == modeMain {
			return m.requestRender()
		}
		return m, nil

	case tea.ColorProfileMsg:
		m.colorProfile = colorProfileString(msg.Profile)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case pickerListedMsg:
		return m.handlePickerListed(msg)

	case pickerProbedMsg:
		return m.handlePickerProbed(msg)

	case runProgressMsg:
		return m.handleRunProgress(msg)

	case runDoneMsg:
		return m.handleRunDone(msg)

	case previewFrameMsg:
		return m.handlePreviewFrame(msg)

	case previewTickMsg:
		return m.handlePreviewTick(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.run.phase != runNone {
		return m.handleRunKey(msg)
	}

	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	if m.modal != nil {
		return m.handleModalKey(msg)
	}

	// '?' opens help, except while the picker's filter is capturing text
	// (where '?' is a filterable character).
	if key == "?" && !(m.mode == modePicker && m.picker.filtering) {
		m.showHelp = true
		return m, nil
	}

	if m.mode == modePicker {
		return m.handlePickerKey(msg)
	}

	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "c":
		m.showFullCommand = !m.showFullCommand
		return m, nil
	case "r":
		return m.tryRun()
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
	case "l":
		return m.seekPreview(1)
	case "h":
		return m.seekPreview(-1)
	case "L":
		return m.seekPreview(5)
	case "H":
		return m.seekPreview(-5)
	case "v":
		return m.togglePreviewMode()
	case "space":
		return m.togglePlay()
	}

	if m.focus == focusMenu {
		switch key {
		case "enter":
			if m.menuCursor == menuRunIndex {
				return m.tryRun()
			}
			m = m.openModal(menuKinds[m.menuCursor])
			return m, nil
		}
	}

	if m.focus == focusPipeline {
		switch key {
		case "x":
			m = m.removeStep()
			return m.maybeRerenderResult()
		case "J":
			m = m.moveStep(1)
			return m.maybeRerenderResult()
		case "K":
			m = m.moveStep(-1)
			return m.maybeRerenderResult()
		case "u":
			m = m.undoLast()
			return m.maybeRerenderResult()
		case "e", "enter":
			m = m.openModalForCurrentStep()
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
	m := New(s, withContext(ctx))
	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
