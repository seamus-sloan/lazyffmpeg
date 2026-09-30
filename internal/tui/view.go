package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/picker"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

const stepLabelWidth = 13

// View renders the current screen.
func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true

	if m.tooSmall() {
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

// tooSmall reports whether the terminal is below the 80x24 the frame
// needs; View then shows only a message saying so.
func (m Model) tooSmall() bool {
	return m.width < 80 || m.height < 24
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

// frameChromeRows counts the main frame's rows outside its two columns
// and the footer's command line(s): the top border, the blank line above
// the footer, the footer's estimate/output/hints line, the bottom border.
const frameChromeRows = 4

// leftColumnFixedRows counts the left column's rows other than the preview
// box's content and the PIPELINE list: the preview box's top and bottom
// border, the info line, the blank line and the PIPELINE heading.
const leftColumnFixedRows = 5

// maxFooterCommandLines is how many lines the footer's expanded command
// may take. The two columns share rows, so the frame is frameChromeRows +
// the command lines + the taller column; the MENU column's height is fixed,
// and the left column needs at least minPreviewRows of preview and one
// PIPELINE row, so whatever is left after the taller of those two is the
// command's to use.
func (m Model) maxFooterCommandLines() int {
	columns := maxInt(len(m.menuLines()), leftColumnFixedRows+minPreviewRows+1)
	return maxInt(m.height-frameChromeRows-columns, 1)
}

// layoutBudget divides the rows left after every fixed line (borders,
// info line, PIPELINE heading, footer) between the preview box and the
// PIPELINE list so the whole frame never exceeds m.height: the preview
// box shrinks first, down to minPreviewRows; once that floor is hit, the
// PIPELINE list itself is capped (and scrolled, see pipelineWindow) down
// to a floor of one row. footerLineCount is at most
// maxFooterCommandLines, which keeps the left column (and the MENU column
// beside it) within the rows that remain.
func (m Model) layoutBudget(footerLineCount, stepCount int) (previewRows, pipelineRows int) {
	available := m.height - frameChromeRows - footerLineCount - leftColumnFixedRows

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
	// 5: "╭─ " before the title, the space after it and the closing "╮".
	title = ansi.Truncate(title, maxInt(width-5, 0), "…")
	remaining := maxInt(width-5-lipgloss.Width(title), 0)
	return frameBorderStyle.Render("╭─ ") + styleTitle(title) + " " +
		frameBorderStyle.Render(strings.Repeat("─", remaining)+"╮")
}

// styleTitle colours a frame title of the form "lazyff · <name>": the
// program name in the accent colour, the separator dimmed, the name bold.
func styleTitle(title string) string {
	if name, ok := strings.CutPrefix(title, "lazyff · "); ok {
		return brandStyle.Render("lazyff") + sep + titleStyle.Render(name)
	}
	return titleStyle.Render(title)
}

func frameBottom(width int) string {
	return frameBorderStyle.Render("╰" + strings.Repeat("─", maxInt(width-2, 0)) + "╯")
}

// boxTop/boxBottom draw a light-weight titled box (the preview box),
// distinct from frameTop/frameBottom's rounded outer frame. title is
// already styled by the caller.
func boxTop(width int, title string) string {
	remaining := maxInt(width-5-lipgloss.Width(title), 0)
	return boxBorderStyle.Render("┌─ ") + title + " " +
		boxBorderStyle.Render(strings.Repeat("─", remaining)+"┐")
}

func boxBottom(width int) string {
	return boxBorderStyle.Render("└" + strings.Repeat("─", maxInt(width-2, 0)) + "┘")
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
	bar := frameBorderStyle.Render("│")
	return bar + c + bar
}

// oneLine collapses text drawn into a single row (an error or status
// message) to its last non-empty line, trimmed: errors from ffmpeg and
// ffprobe carry their stderr, many lines long, and the last is the one
// that says what went wrong. Every frame row then truncates it to the
// width with "…" (see padOrTruncate).
func oneLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
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
	lines = append(lines, panelHeading("PIPELINE", m.focus == focusPipeline))
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
	return timeStyle.Render(pos) + dimStyle.Render(" / ") + timeStyle.Render(total) +
		"  " + titleStyle.Render(dims) + sep + outputStyle.Render(codec) + sep + sizeStyle.Render(size)
}

// panelHeading renders a panel title: accented when that panel has focus,
// dimmed otherwise.
func panelHeading(text string, focused bool) string {
	if focused {
		return focusedHeadingStyle.Render(text)
	}
	return headingStyle.Render(text)
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
		on := m.focus == focusPipeline && i == m.pipelineCursor
		label := s.Kind().Label()
		if len(label) < stepLabelWidth {
			label += strings.Repeat(" ", stepLabelWidth-len(label))
		}
		summary := s.Summary()
		if on {
			summary = cursorStyle.Render(summary)
		}
		lines = append(lines, cursorPrefix(on)+dimStyle.Render(fmt.Sprintf("%d.", i+1))+" "+
			kindStyle(s.Kind()).Render(label)+summary)
	}
	return lines
}

// kindStyle colours a step by what it changes: filter steps (the picture
// and the timeline) in one colour, output settings in another.
func kindStyle(k pipeline.Kind) lipgloss.Style {
	if k.IsFilter() {
		return videoStyle
	}
	return outputStyle
}

func (m Model) menuLines() []string {
	var lines []string
	lines = append(lines, panelHeading("MENU", m.focus == focusMenu))

	idx := 0
	addItem := func(label string, style lipgloss.Style) {
		on := m.focus == focusMenu && idx == m.menuCursor
		if on {
			style = cursorStyle
		}
		lines = append(lines, cursorPrefix(on)+style.Render(label))
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
			lines = append(lines, kindStyle(k).Render(h))
		}
		addItem(menuItemLabel(k), lipgloss.NewStyle())
	}
	lines = append(lines, "")
	addItem("Run", successStyle)

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
	colorLine := colorCommand
	if m.notice != "" {
		raw = oneLine(m.notice)
		colorLine = func(s string) string { return promptStyle.Render(s) }
	} else if argv, err := pipeline.Compile(m.session.Info, m.pipeline, opts); err != nil {
		raw = oneLine(err.Error())
		colorLine = func(s string) string { return errorStyle.Render(s) }
	} else if err := m.inPlaceRenameErr(); err != nil {
		raw = oneLine(err.Error())
		colorLine = func(s string) string { return errorStyle.Render(s) }
	} else {
		raw = pipeline.QuoteCommand(argv)
	}
	commandLines := m.footerCommandLines(raw)
	for i, l := range commandLines {
		commandLines[i] = colorLine(l)
	}

	sizeStr := "–"
	durStr := "--:--"
	if est, eerr := pipeline.Estimate(m.session.Info, m.pipeline, opts); eerr == nil {
		sizeStr = units.FormatSize(est.Bytes)
		durStr = units.FormatClock(est.Duration)
	}
	hints := hintLine("r", "run", "tab", "focus", "?", "help", "q", "quit")
	line2 := sizeStyle.Bold(true).Render("~"+sizeStr) + sep + timeStyle.Render(durStr) +
		dimStyle.Render(" → ") + outputPath + "  " + hints

	return commandLines, line2
}

// inPlaceRenameErr is app.ErrInPlaceRename when an in-place session's
// pipeline has a File name step, else nil: the refusal a run would meet on
// r (see tryRun), shown in the footer up front instead.
func (m Model) inPlaceRenameErr() error {
	if !m.session.InPlace {
		return nil
	}
	if err := app.CheckInPlace(m.session.Input, m.pipeline); errors.Is(err, app.ErrInPlaceRename) {
		return err
	}
	return nil
}

// footerLineCount is the number of lines footerLines' command portion
// currently occupies, used to size the rest of the frame around it.
func (m Model) footerLineCount() int {
	lines, _ := m.footerLines()
	return len(lines)
}

// footerCommandLines is raw truncated to one line, or, when the full
// command is expanded, hard-wrapped across at most maxFooterCommandLines
// lines, the last ending in "…" when even those cannot hold all of it.
func (m Model) footerCommandLines(raw string) []string {
	avail := m.innerWidth()
	if !m.showFullCommand {
		return []string{padOrTruncate(raw, avail)}
	}
	lines := wrapText(raw, avail)
	if limit := m.maxFooterCommandLines(); len(lines) > limit {
		lines = lines[:limit]
		last := []rune(lines[limit-1])
		if len(last) >= avail {
			last = last[:avail-1]
		}
		lines[limit-1] = string(last) + "…"
	}
	return lines
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
		return helpTable(16,
			"j/k, ↑/↓", "move the cursor",
			"enter", "open the directory, or probe the file",
			"backspace,h,←", "go to the parent directory",
			"/", "filter (esc clears, enter accepts)",
			"q, ctrl+c", "quit",
			"?", "toggle this help",
		)
	}
	return helpTable(13,
		"tab", "switch focus between MENU and PIPELINE",
		"j/k, ↑/↓", "move the cursor",
		"enter", "open the selected step's modal",
		"e", "edit the selected pipeline step",
		"x", "remove the selected pipeline step",
		"J/K", "move a filter step down/up",
		"u", "undo the last pipeline change",
		"c", "toggle the full command in the footer",
		"r", "run",
		"l/h", "seek the preview ±1s",
		"L/H", "seek the preview ±5s",
		"v", "toggle original/result preview",
		"space", "play/pause the preview",
		"i/o", "set the trim start/end at the preview position",
		"esc", "cancel a modal, or answer no to a confirmation",
		"q, ctrl+c", "quit",
		"?", "toggle this help",
	)
}

// helpTable lays out key/description pairs under a "Keys" heading, each
// key padded to keyWidth columns and highlighted.
func helpTable(keyWidth int, pairs ...string) string {
	lines := []string{focusedHeadingStyle.Render("Keys")}
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i] + strings.Repeat(" ", maxInt(keyWidth-lipgloss.Width(pairs[i]), 0))
		lines = append(lines, "  "+keyStyle.Render(key)+pairs[i+1])
	}
	return strings.Join(append(lines, "", dimStyle.Render("press any key to close")), "\n")
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
	header = append(header, videoStyle.Render(fmt.Sprint(usable))+dimStyle.Render(" usable "+noun))

	if m.picker.filtering || m.picker.filter != "" {
		header = append(header, dimStyle.Render("filter: ")+promptStyle.Render(m.picker.filter))
	}
	header = append(header, "")

	if m.picker.status != "" {
		header = append(header, errorStyle.Render(oneLine(m.picker.status)))
		header = append(header, "")
	}

	hasParent := m.pickerHasParentRow()
	if len(entries) == 0 {
		lines := append(header, dimStyle.Render(fmt.Sprintf("No video files in %s", m.picker.dir)))
		if hasParent {
			lines = append(lines, keyStyle.Render("backspace")+dimStyle.Render(": go up a directory"))
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
	// 2 (cursor prefix) lead the name; 1 (space) + 10 (size) + 2 (spaces)
	// + 10 (date) trail it.
	w := m.innerWidth() - 2 - 1 - 10 - 2 - 10
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
	// next reports whether the row being built is the cursor row, and
	// advances to the following one.
	next := func() bool {
		on := m.picker.cursor == row
		row++
		return on
	}

	if hasParent {
		on := next()
		name := dimStyle.Render("..")
		if on {
			name = cursorStyle.Render("..")
		}
		lines = append(lines, cursorPrefix(on)+name)
	}
	for _, e := range entries {
		on := next()
		if e.IsDir {
			style := dirStyle
			if on {
				style = cursorStyle
			}
			lines = append(lines, cursorPrefix(on)+style.Render(e.Name+"/"))
		} else {
			name := padOrTruncate(e.Name, nameWidth)
			if on {
				name = cursorStyle.Render(name)
			}
			lines = append(lines, cursorPrefix(on)+name+" "+
				sizeStyle.Render(fmt.Sprintf("%10s", units.FormatSize(e.Size)))+"  "+
				dimStyle.Render(e.ModTime.Format("2006-01-02")))
		}
	}
	return lines
}

// modalBoxStyle borders and pads content shown as a centered overlay (the
// step modal, the help screen).
var modalBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
	BorderForeground(colorAccent).Padding(1, 2)

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
