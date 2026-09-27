package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

const stepLabelWidth = 13

// View renders the current screen.
func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true

	if m.width < 80 || m.height < 24 {
		v.Content = fmt.Sprintf("Terminal too small: need 80×24, have %d×%d", m.width, m.height)
		return v
	}

	body := m.renderFrame()
	if m.showHelp {
		body = m.overlayHelp(body)
	}
	v.Content = body
	return v
}

func (m Model) frameTitle() string {
	return "lazyff · " + filepath.Base(m.session.Input)
}

func (m Model) innerWidth() int {
	w := m.width - 4 // side borders + one column of padding each side
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) leftColumnWidth() int {
	w := m.innerWidth() - menuWidth - 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) previewBoxSize() (cols, rows int) {
	boxWidth := m.leftColumnWidth() - 2
	if boxWidth < 10 {
		boxWidth = 10
	}
	bodyHeight := m.height - 9
	boxRows := bodyHeight - 2
	if boxRows < 3 {
		boxRows = 3
	}
	return boxWidth, boxRows
}

func (m Model) renderFrame() string {
	width := m.width
	title := m.frameTitle()

	left := "╭─ " + title + " "
	remaining := width - lipgloss.Width(left) - 1
	if remaining < 0 {
		remaining = 0
	}
	top := left + strings.Repeat("─", remaining) + "╮"
	bottom := "╰" + strings.Repeat("─", maxInt(width-2, 0)) + "╯"

	leftLines := m.leftColumnLines()
	rightLines := m.menuLines()
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	leftW := m.leftColumnWidth()
	var lines []string
	lines = append(lines, top)
	for i := range leftLines {
		row := padOrTruncate(leftLines[i], leftW) + "  " + padOrTruncate(rightLines[i], menuWidth)
		lines = append(lines, sideLine(width, row))
	}
	lines = append(lines, sideLine(width, ""))

	line1, line2 := m.footerLines()
	lines = append(lines, sideLine(width, padOrTruncate(line1, m.innerWidth())))
	lines = append(lines, sideLine(width, padOrTruncate(line2, m.innerWidth())))
	lines = append(lines, bottom)

	return strings.Join(lines, "\n")
}

func sideLine(width int, content string) string {
	inner := width - 2
	c := padOrTruncate(content, inner)
	return "│" + c + "│"
}

func padOrTruncate(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur > w {
		return truncateToWidth(s, w)
	}
	return s + strings.Repeat(" ", w-cur)
}

func truncateToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// leftColumnLines builds the preview box, info line and PIPELINE panel.
func (m Model) leftColumnLines() []string {
	var lines []string

	cols, rows := m.previewBoxSize()
	lines = append(lines, "┌"+strings.Repeat("─", cols)+"┐")
	for i := 0; i < rows; i++ {
		lines = append(lines, "│"+strings.Repeat(" ", cols)+"│")
	}
	lines = append(lines, "└"+strings.Repeat("─", cols)+"┘")

	lines = append(lines, m.infoLine())
	lines = append(lines, "")
	lines = append(lines, "PIPELINE")
	lines = append(lines, m.pipelineLines()...)

	return lines
}

func (m Model) infoLine() string {
	info := m.session.Info
	pos := units.FormatClock(0)
	total := units.FormatClock(info.Duration)
	dims := fmt.Sprintf("%d×%d", info.Video.Width, info.Video.Height)
	codec := codecLabel(info.Video.Codec)
	size := units.FormatSize(info.SizeBytes)
	return fmt.Sprintf("%s / %s  %s · %s · %s", pos, total, dims, codec, size)
}

func codecLabel(codec string) string {
	switch codec {
	case "h264":
		return "H.264"
	case "hevc":
		return "H.265"
	case "vp9":
		return "VP9"
	case "av1":
		return "AV1"
	}
	return strings.ToUpper(codec)
}

func (m Model) pipelineLines() []string {
	steps := m.pipeline.Steps()
	if len(steps) == 0 {
		return []string{dimStyle.Render("(empty)")}
	}
	lines := make([]string, 0, len(steps))
	for i, s := range steps {
		prefix := "  "
		if m.focus == focusPipeline && i == m.pipelineCursor {
			prefix = "> "
		}
		label := s.Kind().Label()
		if len(label) < stepLabelWidth {
			label += strings.Repeat(" ", stepLabelWidth-len(label))
		}
		lines = append(lines, fmt.Sprintf("%s%d. %s%s", prefix, i+1, label, s.Summary()))
	}
	return lines
}

func (m Model) menuLines() []string {
	var lines []string
	lines = append(lines, "MENU")

	idx := 0
	addItem := func(label string) {
		prefix := "  "
		if m.focus == focusMenu && idx == m.menuCursor {
			prefix = "> "
		}
		lines = append(lines, prefix+label)
		idx++
	}

	lines = append(lines, "Video")
	addItem(menuItemLabel(pipeline.KindResolution))
	addItem(menuItemLabel(pipeline.KindSpeed))
	addItem(menuItemLabel(pipeline.KindTrim))
	addItem(menuItemLabel(pipeline.KindFrameRate))
	lines = append(lines, "")
	lines = append(lines, "Output")
	addItem(menuItemLabel(pipeline.KindEncoder))
	addItem(menuItemLabel(pipeline.KindQuality))
	addItem(menuItemLabel(pipeline.KindAudio))
	addItem(menuItemLabel(pipeline.KindContainer))
	addItem(menuItemLabel(pipeline.KindRawArgs))
	lines = append(lines, "")
	addItem("Run")

	return lines
}

func (m Model) footerLines() (string, string) {
	outputPath := m.session.OutputPath(m.pipeline)
	opts := pipeline.Options{Input: m.session.Input, Output: outputPath}

	var line1 string
	argv, err := pipeline.Compile(m.session.Info, m.pipeline, opts)
	if err != nil {
		line1 = err.Error()
	} else {
		line1 = pipeline.QuoteCommand(argv)
	}
	line1 = m.footerCommandLine(line1)

	sizeStr := "–"
	durStr := "--:--"
	if est, eerr := pipeline.Estimate(m.session.Info, m.pipeline, opts); eerr == nil {
		sizeStr = units.FormatSize(est.Bytes)
		durStr = units.FormatClock(est.Duration)
	}
	hints := "enter run · tab focus · ? help · q quit"
	line2 := fmt.Sprintf("~%s · %s → %s  %s", sizeStr, durStr, outputPath, hints)

	return line1, line2
}

func (m Model) footerCommandLine(raw string) string {
	avail := m.innerWidth()
	if m.showFullCommand {
		return raw
	}
	if lipgloss.Width(raw) > avail {
		return truncateToWidth(raw, maxInt(avail-1, 0)) + "…"
	}
	return raw
}

func helpText() string {
	return strings.Join([]string{
		"Keys",
		"  tab          switch focus between MENU and PIPELINE",
		"  j/k, ↑/↓     move the cursor",
		"  x            remove the selected pipeline step",
		"  J/K          move a filter step down/up",
		"  u            undo the last pipeline change",
		"  c            toggle the full command in the footer",
		"  q, ctrl+c    quit",
		"  ?            toggle this help",
		"",
		"press any key to close",
	}, "\n")
}

func (m Model) overlayHelp(base string) string {
	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(helpText())

	baseW, baseH := lipgloss.Width(base), lipgloss.Height(base)
	mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
	x := maxInt((baseW-mw)/2, 0)
	y := maxInt((baseH-mh)/2, 0)

	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	)
	return c.Render()
}
