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

	var body string
	switch {
	case m.mode == modePicker:
		body = m.renderPickerFrame()
	case m.run.phase != runNone:
		body = m.renderRunFrame()
	default:
		body = m.renderFrame()
	}
	if m.modal != nil {
		body = m.overlayModal(body)
	} else if m.showHelp {
		body = m.overlayHelp(body)
	}
	v.Content = body
	return v
}

func (m Model) frameTitle() string {
	if m.mode == modePicker {
		return "lazyff · " + m.picker.dir
	}
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

func frameTop(width int, title string) string {
	left := "╭─ " + title + " "
	remaining := width - lipgloss.Width(left) - 1
	if remaining < 0 {
		remaining = 0
	}
	return left + strings.Repeat("─", remaining) + "╮"
}

func frameBottom(width int) string {
	return "╰" + strings.Repeat("─", maxInt(width-2, 0)) + "╯"
}

// boxTop/boxBottom draw a light-weight titled box (the preview box),
// distinct from frameTop/frameBottom's rounded outer frame.
func boxTop(width int, title string) string {
	left := "┌─ " + title + " "
	remaining := width - lipgloss.Width(left) - 1
	if remaining < 0 {
		remaining = 0
	}
	return left + strings.Repeat("─", remaining) + "┐"
}

func boxBottom(width int) string {
	return "└" + strings.Repeat("─", maxInt(width-2, 0)) + "┘"
}

func (m Model) renderFrame() string {
	width := m.width
	top := frameTop(width, m.frameTitle())
	bottom := frameBottom(width)

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

// sideLineFlex is sideLine, except a line wider than the frame's inner
// width is left un-truncated (dropping the closing border) rather than
// silently cutting off critical text such as a confirmation prompt's path.
func sideLineFlex(width int, content string) string {
	inner := width - 2
	if lipgloss.Width(content) > inner {
		return "│" + content
	}
	return sideLine(width, content)
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

	lines = append(lines, m.previewBoxLines()...)

	lines = append(lines, m.infoLine())
	lines = append(lines, "")
	lines = append(lines, "PIPELINE")
	lines = append(lines, m.pipelineLines()...)

	return lines
}

func (m Model) infoLine() string {
	info := m.session.Info
	pos := units.FormatClock(m.preview.time)
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
	if m.notice != "" {
		line1 = m.notice
	} else if argv, err := pipeline.Compile(m.session.Info, m.pipeline, opts); err != nil {
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
	hints := "r run · tab focus · ? help · q quit"
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

func (m Model) helpText() string {
	if m.mode == modePicker {
		return strings.Join([]string{
			"Keys",
			"  j/k, ↑/↓        move the cursor",
			"  enter           open the directory, or probe the file",
			"  backspace,h,←   go to the parent directory",
			"  /               filter (esc clears, enter accepts)",
			"  q, ctrl+c       quit",
			"  ?               toggle this help",
			"",
			"press any key to close",
		}, "\n")
	}
	return strings.Join([]string{
		"Keys",
		"  tab          switch focus between MENU and PIPELINE",
		"  j/k, ↑/↓     move the cursor",
		"  enter        open the selected step's modal",
		"  e            edit the selected pipeline step",
		"  x            remove the selected pipeline step",
		"  J/K          move a filter step down/up",
		"  u            undo the last pipeline change",
		"  c            toggle the full command in the footer",
		"  r            run",
		"  l/h          seek the preview ±1s",
		"  L/H          seek the preview ±5s",
		"  v            toggle original/result preview",
		"  space        play/pause the preview",
		"  i/o          set the trim start/end at the preview position",
		"  esc          cancel a modal, or answer no to a confirmation",
		"  q, ctrl+c    quit",
		"  ?            toggle this help",
		"",
		"press any key to close",
	}, "\n")
}

func (m Model) renderPickerFrame() string {
	width := m.width
	top := frameTop(width, m.frameTitle())
	bottom := frameBottom(width)

	var lines []string
	lines = append(lines, top)
	for _, l := range m.pickerBodyLines() {
		lines = append(lines, sideLine(width, padOrTruncate(l, m.innerWidth())))
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

func (m Model) pickerBodyLines() []string {
	var lines []string

	entries := m.pickerVisibleEntries()
	usable := 0
	for _, e := range entries {
		if !e.IsDir {
			usable++
		}
	}
	noun := "files"
	if usable == 1 {
		noun = "file"
	}
	lines = append(lines, fmt.Sprintf("%d usable %s", usable, noun))

	if m.picker.filtering || m.picker.filter != "" {
		lines = append(lines, "filter: "+m.picker.filter)
	}
	lines = append(lines, "")

	if m.picker.status != "" {
		lines = append(lines, errorStyle.Render(m.picker.status))
		lines = append(lines, "")
	}

	hasParent := m.pickerHasParentRow()
	if len(entries) == 0 {
		lines = append(lines, fmt.Sprintf("No video files in %s", m.picker.dir))
		if hasParent {
			lines = append(lines, "backspace: go up a directory")
		}
		return lines
	}

	row := 0
	if hasParent {
		prefix := "  "
		if m.picker.cursor == row {
			prefix = "> "
		}
		lines = append(lines, prefix+"..")
		row++
	}
	for _, e := range entries {
		prefix := "  "
		if m.picker.cursor == row {
			prefix = "> "
		}
		if e.IsDir {
			lines = append(lines, fmt.Sprintf("%s%s/", prefix, e.Name))
		} else {
			lines = append(lines, fmt.Sprintf("%s%-40s %10s  %s", prefix, e.Name,
				units.FormatSize(e.Size), e.ModTime.Format("2006-01-02")))
		}
		row++
	}
	return lines
}

func (m Model) overlayHelp(base string) string {
	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(m.helpText())

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
