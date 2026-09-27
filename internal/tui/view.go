package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/picker"
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
		body = overlay(body, modalBoxStyle.Render(m.modalView()))
	} else if m.showHelp {
		body = overlay(body, modalBoxStyle.Render(m.helpText()))
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

// minPreviewRows is the smallest the preview box's content area ever
// shrinks to when the frame is squeezed to fit the terminal height.
const minPreviewRows = 3

// layoutBudget divides the rows left after every fixed line (borders,
// info line, PIPELINE heading, footer) between the preview box and the
// PIPELINE list so the whole frame never exceeds m.height: the preview
// box shrinks first, down to minPreviewRows; once that floor is hit, the
// PIPELINE list itself is capped (and scrolled, see pipelineWindow) down
// to a floor of one row.
func (m Model) layoutBudget(footerLineCount, stepCount int) (previewRows, pipelineRows int) {
	// 3: top border, the blank line above the footer, bottom border.
	// 2: the preview box's own top/bottom border.
	// 1+1+1: info line, blank line, "PIPELINE" heading.
	// footerLineCount + 1: the footer's command line(s) plus its second
	// (estimate/output/hints) line, which footerLineCount does not count.
	fixed := 3 + 2 + 1 + 1 + 1 + footerLineCount + 1
	available := m.height - fixed

	pipelineWant := maxInt(1, stepCount)
	if available-pipelineWant >= minPreviewRows {
		return available - pipelineWant, pipelineWant
	}
	previewRows = minPreviewRows
	pipelineRows = available - previewRows
	if pipelineRows < 1 {
		pipelineRows = 1
	}
	return previewRows, pipelineRows
}

func (m Model) previewBoxSize() (cols, rows int) {
	boxWidth := m.leftColumnWidth() - 2
	if boxWidth < 10 {
		boxWidth = 10
	}
	rows, _ = m.layoutBudget(m.footerLineCount(), m.pipeline.Len())
	if rows < minPreviewRows {
		rows = minPreviewRows
	}
	return boxWidth, rows
}

// pipelineWindow returns the [start,end) slice of a stepCount-long
// PIPELINE list that fits within visible rows while keeping cursor in
// view, scrolling only as far as needed.
func pipelineWindow(stepCount, visible, cursor int) (start, end int) {
	if visible >= stepCount {
		return 0, stepCount
	}
	if visible < 1 {
		visible = 1
	}
	start = cursor - visible/2
	if start+visible > stepCount {
		start = stepCount - visible
	}
	if start < 0 {
		start = 0
	}
	return start, start + visible
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

	commandLines, line2 := m.footerLines()
	for _, cl := range commandLines {
		lines = append(lines, sideLine(width, padOrTruncate(cl, m.innerWidth())))
	}
	lines = append(lines, sideLine(width, padOrTruncate(line2, m.innerWidth())))
	lines = append(lines, bottom)

	return strings.Join(lines, "\n")
}

func sideLine(width int, content string) string {
	inner := width - 2
	c := padOrTruncate(content, inner)
	return "│" + c + "│"
}

// padOrTruncate pads s with trailing spaces to width w, or truncates it
// (ANSI- and wide-character-aware, with a trailing "…") when it is wider.
func padOrTruncate(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-cur)
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
	heading := "PIPELINE"
	if m.focus == focusPipeline {
		heading = focusedHeadingStyle.Render(heading)
	}
	lines = append(lines, heading)
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
	_, maxRows := m.layoutBudget(m.footerLineCount(), len(steps))
	start, end := pipelineWindow(len(steps), maxRows, m.pipelineCursor)

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		s := steps[i]
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
	heading := "MENU"
	if m.focus == focusMenu {
		heading = focusedHeadingStyle.Render(heading)
	}
	lines = append(lines, heading)

	idx := 0
	addItem := func(label string) {
		prefix := "  "
		if m.focus == focusMenu && idx == m.menuCursor {
			prefix = "> "
		}
		lines = append(lines, prefix+label)
		idx++
	}

	// menuSectionHeadings names the heading inserted right before the
	// menuKinds item at that index (0 = "Video", the first section, so it
	// gets no separating blank line; every later heading does).
	menuSectionHeadings := map[int]string{0: "Video", 4: "Output"}
	for i, k := range menuKinds {
		if h, ok := menuSectionHeadings[i]; ok {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, h)
		}
		addItem(menuItemLabel(k))
	}
	lines = append(lines, "")
	addItem("Run")

	return lines
}

// footerLines returns the footer's command line(s) (a single truncated
// line normally, or as many hard-wrapped lines as needed when the full
// command is expanded) and its second line (estimate, output path, key
// hints).
func (m Model) footerLines() ([]string, string) {
	outputPath := m.session.OutputPath(m.pipeline)
	opts := pipeline.Options{Input: m.session.Input, Output: outputPath}

	var raw string
	if m.notice != "" {
		raw = m.notice
	} else if argv, err := pipeline.Compile(m.session.Info, m.pipeline, opts); err != nil {
		raw = err.Error()
	} else {
		raw = pipeline.QuoteCommand(argv)
	}
	commandLines := m.footerCommandLines(raw)

	sizeStr := "–"
	durStr := "--:--"
	if est, eerr := pipeline.Estimate(m.session.Info, m.pipeline, opts); eerr == nil {
		sizeStr = units.FormatSize(est.Bytes)
		durStr = units.FormatClock(est.Duration)
	}
	hints := "r run · tab focus · ? help · q quit"
	line2 := fmt.Sprintf("~%s · %s → %s  %s", sizeStr, durStr, outputPath, hints)

	return commandLines, line2
}

// footerLineCount is the number of lines footerLines' command portion
// currently occupies, used to size the rest of the frame around it.
func (m Model) footerLineCount() int {
	lines, _ := m.footerLines()
	return len(lines)
}

func (m Model) footerCommandLines(raw string) []string {
	avail := m.innerWidth()
	if !m.showFullCommand {
		return []string{padOrTruncate(raw, avail)}
	}
	return wrapText(raw, avail)
}

// wrapText hard-wraps s into chunks of at most width runes each (the
// footer's expanded command has no ANSI styling to worry about breaking).
func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	var lines []string
	for len(r) > width {
		lines = append(lines, string(r[:width]))
		r = r[width:]
	}
	lines = append(lines, string(r))
	return lines
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
	var header []string

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
	header = append(header, fmt.Sprintf("%d usable %s", usable, noun))

	if m.picker.filtering || m.picker.filter != "" {
		header = append(header, "filter: "+m.picker.filter)
	}
	header = append(header, "")

	if m.picker.status != "" {
		header = append(header, errorStyle.Render(m.picker.status))
		header = append(header, "")
	}

	hasParent := m.pickerHasParentRow()
	if len(entries) == 0 {
		lines := append(header, fmt.Sprintf("No video files in %s", m.picker.dir))
		if hasParent {
			lines = append(lines, "backspace: go up a directory")
		}
		return lines
	}

	rows := m.pickerRowLines(entries, hasParent)
	maxRows := m.height - 2 - len(header)
	if maxRows < 1 {
		maxRows = 1
	}
	start, end := pipelineWindow(len(rows), maxRows, m.picker.cursor)

	return append(header, rows[start:end]...)
}

// pickerNameWidth is the fixed width the name column is padded/truncated
// to, so the size and date columns that follow line up regardless of how
// long any one entry's name is.
func (m Model) pickerNameWidth() int {
	// 1 (space) + 10 (size) + 2 (spaces) + 10 (date) trail the name.
	w := m.innerWidth() - 1 - 10 - 2 - 10
	if w < 10 {
		w = 10
	}
	return w
}

// pickerRowLines renders every row (the ".." parent row, if any, then one
// row per entry), independent of which rows are actually visible.
func (m Model) pickerRowLines(entries []picker.Entry, hasParent bool) []string {
	nameWidth := m.pickerNameWidth()
	var lines []string

	row := 0
	prefixFor := func() string {
		prefix := "  "
		if m.picker.cursor == row {
			prefix = "> "
		}
		row++
		return prefix
	}

	if hasParent {
		lines = append(lines, prefixFor()+"..")
	}
	for _, e := range entries {
		prefix := prefixFor()
		if e.IsDir {
			lines = append(lines, fmt.Sprintf("%s%s/", prefix, e.Name))
		} else {
			name := padOrTruncate(e.Name, nameWidth)
			lines = append(lines, fmt.Sprintf("%s%s %10s  %s", prefix, name,
				units.FormatSize(e.Size), e.ModTime.Format("2006-01-02")))
		}
	}
	return lines
}

// modalBoxStyle borders and pads content shown as a centered overlay (the
// step modal, the help screen).
var modalBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)

// overlay centers content (already styled as a box by its caller, e.g.
// modalBoxStyle) over base using a lipgloss compositor.
func overlay(base, content string) string {
	baseW, baseH := lipgloss.Width(base), lipgloss.Height(base)
	cw, ch := lipgloss.Width(content), lipgloss.Height(content)
	x := maxInt((baseW-cw)/2, 0)
	y := maxInt((baseH-ch)/2, 0)

	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(content).X(x).Y(y).Z(1),
	)
	return c.Render()
}
