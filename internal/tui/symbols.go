package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// ClipboardFunc writes text to the system clipboard. The default is
// clipboard.WriteAll (pbcopy, xclip, wl-copy, ...); tests inject a fake
// via WithClipboard.
type ClipboardFunc func(text string) error

// WithClipboard overrides the function used to write to the clipboard.
func WithClipboard(fn ClipboardFunc) Option {
	return func(m *Model) { m.clipboardFn = fn }
}

// symbolsCopiedMsg carries the outcome of copying the preview's frame as
// plain-text symbols (see copySymbols). text is set when the render
// worked but the system clipboard did not, so it can go through the
// terminal's clipboard instead.
type symbolsCopiedMsg struct {
	time       float64
	cols, rows int
	text       string
	err        error
}

// copySymbols renders the frame the preview shows as plain-text symbols
// as wide as the terminal (see preview.Request.Plain) and copies them to
// the clipboard, reporting how it went in the footer.
func (m Model) copySymbols() (Model, tea.Cmd) {
	if !m.rendererAvailable {
		m.notice = "install chafa to copy the frame as symbols"
		return m, nil
	}
	t, filter := m.previewFrame()
	req := preview.Request{Input: m.session.Input, Time: t, Filter: filter, Cols: m.width, Plain: true}
	fn, clip, ctx := m.renderFn, m.clipboardFn, m.ctx
	m.notice = "copying the frame as symbols…"
	return m, func() tea.Msg {
		text, err := fn(ctx, req)
		if err != nil {
			return symbolsCopiedMsg{err: err}
		}
		msg := symbolsCopiedMsg{time: req.Time, cols: req.Cols, rows: strings.Count(text, "\n") + 1}
		if clip(text) != nil {
			msg.text = text
		}
		return msg
	}
}

func (m Model) handleSymbolsCopied(msg symbolsCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "copying the frame failed: " + oneLine(msg.err.Error())
		return m, nil
	}
	m.notice = fmt.Sprintf("copied the frame at %s as %d×%d symbols", units.FormatClock(msg.time), msg.cols, msg.rows)
	if msg.text != "" {
		// No system clipboard (e.g. over SSH): ask the terminal to take it.
		m.notice += " (via the terminal)"
		return m, tea.SetClipboard(msg.text)
	}
	return m, nil
}
