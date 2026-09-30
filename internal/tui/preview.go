package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
)

// RenderFunc renders one preview frame. The default is preview.Render;
// tests inject a fake via WithRenderer.
type RenderFunc func(ctx context.Context, req preview.Request) (string, error)

// WithRenderer overrides the function (and its availability) used to
// render preview frames.
func WithRenderer(fn RenderFunc, available bool) Option {
	return func(m *Model) {
		m.renderFn = fn
		m.rendererAvailable = available
	}
}

// previewState is the live preview's state on the main screen.
type previewState struct {
	time       float64 // current position, in input-timeline seconds
	resultMode bool    // false = original, true = result
	playing    bool
	playGen    int // bumped each time playback is paused, so a tick from a
	// since-superseded play session (still ticking down from before it was
	// paused) is recognized as stale and dropped rather than re-armed

	frame    string
	frameErr string

	// image is the latest kitty graphics render (see preview.Request's
	// ImageID): the sequence that draws it, the id it is transmitted
	// under and the box size, in cells, it was rendered for. drawnID is
	// the id of the image on screen now, 0 = none (see syncImage).
	image                string
	imageID              int
	imageCols, imageRows int
	drawnID              int

	nextSeq    int // monotonically increasing request counter
	pendingSeq int // seq of the outstanding render; 0 = none in flight
	dirty      bool

	// cols, rows is the preview box size the latest render request was
	// made for (see rerenderIfBoxResized).
	cols, rows int
}

// previewFrameMsg carries one render's outcome, tagged with the request's
// sequence number so a reply that lands after a newer request was issued
// can be dropped as stale, and with the request itself.
type previewFrameMsg struct {
	seq  int
	req  preview.Request
	text string
	err  error
}

// imageIDs are the two kitty image ids preview frames alternate between,
// so each new frame is drawn before the one it replaces is deleted
// (without a blank gap between them). They are unusual enough ("lf" in
// the high bytes) not to collide with images other programs leave on the
// screen, since Run deletes both on exit.
var imageIDs = [2]int{0x6c660001, 0x6c660002}

// graphicsProbeID is the image id of the kitty graphics query sent at
// startup; a terminal that supports the protocol answers it with "OK".
const graphicsProbeID = 0x6c660000

// graphicsProbe asks the terminal whether it supports kitty graphics (a
// query for a 1x1 image that is never stored or shown) and for its window
// size in pixels, which previewPixelSize divides between cells.
var graphicsProbe = ansi.KittyGraphics([]byte("AAAA"),
	"i="+strconv.Itoa(graphicsProbeID), "s=1", "v=1", "a=q", "t=d", "f=24") +
	ansi.WindowOp(14)

// fallbackCellWidth x fallbackCellHeight is the cell size, in pixels,
// assumed when the terminal never reported its window size in pixels.
const fallbackCellWidth, fallbackCellHeight = 8, 16

// previewTickMsg advances playback by one frame interval. gen ties it to
// the play session that scheduled it (see previewState.playGen).
type previewTickMsg struct{ gen int }

const previewTickInterval = 125 * time.Millisecond

func previewTickCmd(gen int) tea.Cmd {
	return tea.Tick(previewTickInterval, func(time.Time) tea.Msg { return previewTickMsg{gen: gen} })
}

// previewRange returns the current preview mode's playable input-time
// range: the full input in original mode, PlayableRange in result mode.
func (m Model) previewRange() (float64, float64) {
	if m.preview.resultMode {
		return pipeline.PlayableRange(m.session.Info, m.pipeline)
	}
	return 0, m.session.Info.Duration
}

func (m Model) previewRequest() preview.Request {
	cols, rows := m.previewBoxSize()
	filter := ""
	if m.preview.resultMode {
		filter = pipeline.SpatialVideoFilter(m.pipeline)
	}
	lo, hi := m.previewRange()
	req := preview.Request{
		Input:  m.session.Input,
		Time:   clampRenderTime(m.preview.time, lo, hi, m.session.Info.Video.FPS),
		Filter: filter,
		Cols:   cols,
		Rows:   rows,
		Colors: m.colorProfile,
	}
	if m.graphics {
		req.ImageID = imageIDs[0]
		if m.preview.imageID == imageIDs[0] {
			req.ImageID = imageIDs[1]
		}
		req.PixelWidth, req.PixelHeight = m.previewPixelSize(cols, rows)
	}
	return req
}

// previewPixelSize returns the size, in pixels, of a cols x rows box: the
// terminal's window size in pixels divided evenly between its cells, or
// fallbackCellWidth x fallbackCellHeight cells when it never reported one.
func (m Model) previewPixelSize(cols, rows int) (w, h int) {
	if m.windowPixelWidth <= 0 || m.windowPixelHeight <= 0 || m.width <= 0 || m.height <= 0 {
		return cols * fallbackCellWidth, rows * fallbackCellHeight
	}
	return cols * m.windowPixelWidth / m.width, rows * m.windowPixelHeight / m.height
}

// clampRenderTime clamps t into [lo,hi], except its effective upper bound
// is max(lo, hi-1/fps) rather than hi itself: a request for a frame at or
// past the clip's exact duration can decode no frame at all, so the
// render always asks for a moment strictly before the range's end. fps
// falls back to 30 when the input's frame rate is unknown.
func clampRenderTime(t, lo, hi, fps float64) float64 {
	if fps <= 0 {
		fps = 30
	}
	upper := hi - 1/fps
	if upper < lo {
		upper = lo
	}
	return clampFloat(t, lo, upper)
}

// requestRender asks for a fresh frame at the preview's current state. At
// most one render is ever in flight: a request that arrives while one is
// already outstanding just marks the preview dirty, so exactly one more
// render (at whatever the state is by then) follows once the in-flight one
// lands.
func (m Model) requestRender() (Model, tea.Cmd) {
	if !m.canRender() || m.session.Input == "" {
		return m, nil
	}
	if m.preview.pendingSeq != 0 {
		m.preview.dirty = true
		return m, nil
	}
	m.preview.nextSeq++
	seq := m.preview.nextSeq
	m.preview.pendingSeq = seq
	m.preview.dirty = false
	req := m.previewRequest()
	m.preview.cols, m.preview.rows = req.Cols, req.Rows
	return m, m.renderCmd(seq, req)
}

// rerenderIfBoxResized requests a fresh render whenever the preview box's
// size no longer matches the size the latest render was requested for.
// The box grows and shrinks with the terminal, the pipeline's length and
// the footer's expanded command, so this runs after every Update rather
// than at each place that can change one of those: a frame is never left
// on screen cropped (or padded) to a size it was not rendered for without
// a render for the new size on its way.
func (m Model) rerenderIfBoxResized() (Model, tea.Cmd) {
	if m.mode != modeMain || m.run.phase != runNone || m.width == 0 || m.preview.dirty {
		return m, nil
	}
	cols, rows := m.previewBoxSize()
	if cols == m.preview.cols && rows == m.preview.rows {
		return m, nil
	}
	return m.requestRender()
}

// canRender reports whether preview frames can be rendered at all: as
// kitty images, which need only ffmpeg, or as chafa symbols.
func (m Model) canRender() bool {
	return m.graphics || m.rendererAvailable
}

func (m Model) renderCmd(seq int, req preview.Request) tea.Cmd {
	fn := m.renderFn
	ctx := m.ctx
	return func() tea.Msg {
		text, err := fn(ctx, req)
		return previewFrameMsg{seq: seq, req: req, text: text, err: err}
	}
}

func (m Model) handlePreviewFrame(msg previewFrameMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.preview.pendingSeq {
		return m, nil // superseded by a newer request
	}
	m.preview.pendingSeq = 0
	switch {
	case msg.err != nil:
		m.preview.frameErr = msg.err.Error()
		m.preview.frame = ""
		m.preview.image = ""
	case msg.req.ImageID != 0:
		m.preview.frameErr = ""
		m.preview.image = msg.text
		m.preview.imageID = msg.req.ImageID
		m.preview.imageCols, m.preview.imageRows = msg.req.Cols, msg.req.Rows
	default:
		m.preview.frameErr = ""
		m.preview.frame = msg.text
	}
	if m.preview.dirty {
		return m.requestRender()
	}
	return m, nil
}

func clampFloat(v, lo, hi float64) float64 {
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

func (m Model) seekPreview(delta float64) (Model, tea.Cmd) {
	lo, hi := m.previewRange()
	m.preview.time = clampFloat(m.preview.time+delta, lo, hi)
	return m.requestRender()
}

func (m Model) togglePreviewMode() (Model, tea.Cmd) {
	m.preview.resultMode = !m.preview.resultMode
	m = m.clampPreviewTime()
	return m.requestRender()
}

func (m Model) togglePlay() (Model, tea.Cmd) {
	m.preview.playing = !m.preview.playing
	if m.preview.playing {
		return m, previewTickCmd(m.preview.playGen)
	}
	m.preview.playGen++ // invalidate any tick still ticking down from this session
	return m, nil
}

func (m Model) handlePreviewTick(msg previewTickMsg) (tea.Model, tea.Cmd) {
	if m.run.phase != runNone || !m.preview.playing || msg.gen != m.preview.playGen {
		return m, nil
	}
	rate := 1.0
	if m.preview.resultMode {
		rate = pipeline.PlaybackRate(m.pipeline)
	}
	_, hi := m.previewRange()
	t := m.preview.time + float64(previewTickInterval)/float64(time.Second)*rate

	var cmds []tea.Cmd
	if t >= hi {
		t = hi
		m.preview.playing = false
	} else {
		cmds = append(cmds, previewTickCmd(m.preview.playGen))
	}
	m.preview.time = t

	mm, renderCmd := m.requestRender()
	m = mm
	if renderCmd != nil {
		cmds = append(cmds, renderCmd)
	}
	return m, tea.Batch(cmds...)
}

// setTrimBound sets the Trim step's Start (isStart) or End at the current
// preview position, keeping the other bound, and upserting a new Trim
// step (appended after the pipeline's other filter steps) when there is
// none yet. Because a Trim step placed after a Speed step works in
// sped-up time, the input-time preview position is mapped through
// whatever filter steps precede it. MapTime passes non-filter steps
// through unchanged, so mapping through every step (when there is no
// existing Trim step to stop at) is as good as mapping through only the
// leading filters.
func (m Model) setTrimBound(isStart bool) (Model, tea.Cmd) {
	steps := m.pipeline.Steps()
	before := len(steps)
	for i, s := range steps {
		if s.Kind() == pipeline.KindTrim {
			before = i
			break
		}
	}

	t := pipeline.MapTime(m.pipeline, before, m.preview.time)

	var trim pipeline.Trim
	if cur, ok := m.pipeline.Find(pipeline.KindTrim); ok {
		trim = cur.(pipeline.Trim)
	}
	if isStart {
		trim.Start = t
	} else {
		trim.End = t
	}
	if trim.Validate() != nil {
		return m, nil
	}

	m = m.pushUndo()
	m.pipeline = m.pipeline.Upsert(trim)
	return m.maybeRerenderResult()
}

// maybeRerenderResult requests a fresh render after a pipeline edit, but
// only when the preview is showing the result (the original frame is
// unaffected by pipeline changes). An edit can shrink the result's
// playable range (a Trim step, a faster Speed), so the preview position
// itself is clamped into the new range first: the info line, playback and
// i/o all read it, not just the render.
func (m Model) maybeRerenderResult() (Model, tea.Cmd) {
	if m.mode != modeMain || !m.preview.resultMode {
		return m, nil
	}
	m = m.clampPreviewTime()
	return m.requestRender()
}

// clampPreviewTime clamps the preview position into the current preview
// mode's playable range.
func (m Model) clampPreviewTime() Model {
	lo, hi := m.previewRange()
	m.preview.time = clampFloat(m.preview.time, lo, hi)
	return m
}

func colorProfileString(p colorprofile.Profile) string {
	switch p {
	case colorprofile.TrueColor:
		return "full"
	case colorprofile.ANSI256:
		return "256"
	case colorprofile.ANSI:
		return "16"
	}
	return "none"
}

func (m Model) previewBoxLines() []string {
	cols, rows := m.previewBoxSize()
	label := videoStyle.Render("original")
	if m.preview.resultMode {
		label = successStyle.Render("result")
	}

	// A kitty image is drawn over the box's (blank) content by syncImage.
	var content []string
	switch {
	case !m.canRender():
		content = []string{dimStyle.Render("install chafa for preview")}
	case m.preview.frameErr != "":
		content = []string{errorStyle.Render(oneLine(m.preview.frameErr))}
	case m.preview.frame != "" && !m.graphics:
		content = strings.Split(m.preview.frame, "\n")
	}

	bar := boxBorderStyle.Render("│")
	lines := []string{boxTop(cols+2, label)}
	for i := 0; i < rows; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, bar+padOrTruncate(text, cols)+bar)
	}
	lines = append(lines, boxBottom(cols+2))
	return lines
}

// previewOrigin is the screen position (0-based column, row) of the
// preview box's first content cell: inside the main frame's left border
// and top border, and the preview box's own.
func previewOrigin() (x, y int) {
	return 2, 2
}

// imageVisible reports whether the preview's kitty image belongs on
// screen: on the main screen with nothing drawn over the preview box, and
// only while the image was rendered for the box's current size (a stale
// one could spill past a box that has since shrunk).
func (m Model) imageVisible() bool {
	if !m.graphics || m.preview.image == "" || m.quitting || m.tooSmall() ||
		m.mode != modeMain || m.run.phase != runNone || m.modal != nil || m.showHelp {
		return false
	}
	cols, rows := m.previewBoxSize()
	return m.preview.imageCols == cols && m.preview.imageRows == rows
}

// syncImage draws or deletes the preview's kitty image so what is on
// screen matches imageVisible and the latest image. Bubble Tea's renderer
// only ever draws text, so the image is written straight to the terminal,
// at previewOrigin (the cursor is saved and restored around it, so the
// renderer's idea of where it is stays true); it runs after every Update,
// like rerenderIfBoxResized, rather than at each place that can change
// whether the image belongs on screen. A new image is drawn before the
// one it replaces is deleted, so frames change without a blank between.
func (m Model) syncImage() (Model, tea.Cmd) {
	want := 0
	if m.imageVisible() {
		want = m.preview.imageID
	}
	if want == m.preview.drawnID {
		return m, nil
	}
	var seq string
	if want != 0 {
		x, y := previewOrigin()
		seq = ansi.SaveCursor + ansi.CursorPosition(x+1, y+1) + m.preview.image + ansi.RestoreCursor
	}
	if m.preview.drawnID != 0 {
		seq += preview.DeleteKittyImage(m.preview.drawnID)
	}
	m.preview.drawnID = want
	return m, tea.Raw(seq)
}
