package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/picker"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// ListFunc lists one directory for the file picker.
type ListFunc func(dir string) ([]picker.Entry, error)

// ProbeFunc probes a chosen file for the file picker.
type ProbeFunc func(ctx context.Context, path string) (probe.Info, error)

// pickerState is the file picker's state, active while Model.mode ==
// modePicker.
type pickerState struct {
	dir     string
	entries []picker.Entry
	cursor  int

	filtering bool
	filter    string

	status      string
	probing     bool
	probingPath string // the file a pending probe was started for
}

// pickerListedMsg reports the result of an async directory listing.
type pickerListedMsg struct {
	dir     string
	entries []picker.Entry
	err     error
}

// pickerProbedMsg reports the result of an async probe of a chosen file.
type pickerProbedMsg struct {
	path string
	info probe.Info
	err  error
}

func (m Model) listCmd() tea.Cmd {
	dir := m.picker.dir
	fn := m.listFn
	return func() tea.Msg {
		entries, err := fn(dir)
		return pickerListedMsg{dir: dir, entries: entries, err: err}
	}
}

func (m Model) probeCmd(path string) tea.Cmd {
	fn := m.probeFn
	ctx := m.ctx
	return func() tea.Msg {
		info, err := fn(ctx, path)
		return pickerProbedMsg{path: path, info: info, err: err}
	}
}

func (m Model) handlePickerListed(msg pickerListedMsg) (tea.Model, tea.Cmd) {
	if msg.dir != m.picker.dir {
		return m, nil // stale: the user has since navigated elsewhere
	}
	if msg.err != nil {
		m.picker.status = msg.err.Error()
		m.picker.entries = nil
		return m, nil
	}
	m.picker.status = ""
	m.picker.entries = msg.entries
	m.picker.cursor = m.pickerInitialCursor()
	return m, nil
}

// pickerInitialCursor is the cursor row a fresh directory listing starts
// on: the first real entry, skipping the ".." row, unless ".." is the
// only row there is.
func (m Model) pickerInitialCursor() int {
	if m.pickerHasParentRow() && len(m.pickerVisibleEntries()) > 0 {
		return 1
	}
	return 0
}

func (m Model) handlePickerProbed(msg pickerProbedMsg) (tea.Model, tea.Cmd) {
	if m.mode != modePicker {
		return m, nil // stale: the editor has since moved on to another file
	}
	if m.picker.probing && msg.path != m.picker.probingPath {
		return m, nil // stale: superseded by a newer probe request
	}
	m.picker.probing = false
	if msg.err != nil {
		m.picker.status = msg.err.Error()
		return m, nil
	}

	m.session = app.Session{
		Input:    msg.path,
		Info:     msg.info,
		Pipeline: m.pipeline,
		Output:   m.session.Output,
		InPlace:  m.session.InPlace,
		Force:    m.session.Force,
	}
	m.mode = modeMain
	m.focus = focusMenu
	m.menuCursor = 0
	m.pipelineCursor = 0
	m.undo = nil
	m.preview = previewState{}
	m.picker.status = ""

	if m.width > 0 {
		return m.requestRender()
	}
	return m, nil
}

func (m Model) pickerHasParentRow() bool {
	return filepath.Dir(m.picker.dir) != m.picker.dir
}

func (m Model) pickerVisibleEntries() []picker.Entry {
	if m.picker.filter == "" {
		return m.picker.entries
	}
	needle := strings.ToLower(m.picker.filter)
	var out []picker.Entry
	for _, e := range m.picker.entries {
		if strings.Contains(strings.ToLower(e.Name), needle) {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) pickerRowCount() int {
	n := len(m.pickerVisibleEntries())
	if m.pickerHasParentRow() {
		n++
	}
	return n
}

func (m Model) movePickerCursor(delta int) Model {
	n := m.pickerRowCount()
	if n == 0 {
		m.picker.cursor = 0
		return m
	}
	m.picker.cursor = clamp(m.picker.cursor+delta, 0, n-1)
	return m
}

func (m Model) pickerGoParent() (Model, tea.Cmd) {
	parent := filepath.Dir(m.picker.dir)
	if parent == m.picker.dir {
		return m, nil
	}
	m.picker.dir = parent
	m.picker.cursor = 0
	m.picker.filter = ""
	m.picker.filtering = false
	m.picker.status = ""
	return m, m.listCmd()
}

func (m Model) pickerOpenDir(dir string) (Model, tea.Cmd) {
	m.picker.dir = dir
	m.picker.cursor = 0
	m.picker.filter = ""
	m.picker.filtering = false
	m.picker.status = ""
	return m, m.listCmd()
}

func (m Model) pickerActivate() (Model, tea.Cmd) {
	if m.picker.probing {
		return m, nil // a probe is already pending; ignore another enter
	}
	entries := m.pickerVisibleEntries()
	idx := m.picker.cursor
	if m.pickerHasParentRow() {
		if idx == 0 {
			return m.pickerGoParent()
		}
		idx--
	}
	if idx < 0 || idx >= len(entries) {
		return m, nil
	}
	e := entries[idx]
	if e.IsDir {
		return m.pickerOpenDir(e.Path)
	}
	m.picker.status = ""
	m.picker.probing = true
	m.picker.probingPath = e.Path
	return m, m.probeCmd(e.Path)
}

func (m Model) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if m.picker.filtering {
		switch k {
		case "esc":
			m.picker.filtering = false
			m.picker.filter = ""
			m.picker.cursor = 0
			return m, nil
		case "enter":
			m.picker.filtering = false
			return m, nil
		case "backspace":
			if r := []rune(m.picker.filter); len(r) > 0 {
				m.picker.filter = string(r[:len(r)-1])
			}
			m.picker.cursor = 0
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up":
			m = m.movePickerCursor(-1)
			return m, nil
		case "down":
			m = m.movePickerCursor(1)
			return m, nil
		}
		if msg.Text != "" {
			m.picker.filter += msg.Text
			m.picker.cursor = 0
			return m, nil
		}
		return m, nil
	}

	switch k {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "j", "down":
		m = m.movePickerCursor(1)
		return m, nil
	case "k", "up":
		m = m.movePickerCursor(-1)
		return m, nil
	case "/":
		m.picker.filtering = true
		return m, nil
	case "backspace", "h", "left":
		return m.pickerGoParent()
	case "enter":
		return m.pickerActivate()
	}
	return m, nil
}
