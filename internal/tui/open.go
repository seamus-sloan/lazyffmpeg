package tui

import (
	"os/exec"
	"path/filepath"
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// OpenFunc opens a file in the system's default app for it. The default
// is openFile; tests inject a fake via WithOpener.
type OpenFunc func(path string) error

// WithOpener overrides the function used to open a run's output.
func WithOpener(fn OpenFunc) Option {
	return func(m *Model) { m.openFn = fn }
}

// openFile hands path to the system's opener (open on macOS, start on
// Windows, xdg-open elsewhere), without waiting for the app it launches.
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reap the opener; its outcome is the app's to report
	return nil
}

// openedMsg carries the outcome of opening a run's output.
type openedMsg struct {
	path string
	err  error
}

// openOutput opens the finished run's output (see OpenFunc) and closes the
// done screen; the footer says how it went.
func (m Model) openOutput() (Model, tea.Cmd) {
	path, fn := m.run.result.Output, m.openFn
	m.run = runState{}
	return m, func() tea.Msg {
		return openedMsg{path: path, err: fn(path)}
	}
}

func (m Model) handleOpened(msg openedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "opening " + filepath.Base(msg.path) + " failed: " + oneLine(msg.err.Error())
		return m, nil
	}
	m.notice = "opened " + filepath.Base(msg.path)
	return m, nil
}
