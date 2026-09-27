package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

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

	nextSeq    int // monotonically increasing request counter
	pendingSeq int // seq of the outstanding render; 0 = none in flight
	dirty      bool
}

// previewFrameMsg carries one render's outcome, tagged with the request's
// sequence number so a reply that lands after a newer request was issued
// can be dropped as stale.
type previewFrameMsg struct {
	seq  int
	text string
	err  error
}

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
	return preview.Request{
		Input:  m.session.Input,
		Time:   clampRenderTime(m.preview.time, lo, hi, m.session.Info.Video.FPS),
		Filter: filter,
		Cols:   cols,
		Rows:   rows,
		Colors: m.colorProfile,
	}
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
	if !m.rendererAvailable || m.session.Input == "" {
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
	return m, m.renderCmd(seq, m.previewRequest())
}

func (m Model) renderCmd(seq int, req preview.Request) tea.Cmd {
	fn := m.renderFn
	ctx := m.ctx
	return func() tea.Msg {
		text, err := fn(ctx, req)
		return previewFrameMsg{seq: seq, text: text, err: err}
	}
}

func (m Model) handlePreviewFrame(msg previewFrameMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.preview.pendingSeq {
		return m, nil // superseded by a newer request
	}
	m.preview.pendingSeq = 0
	if msg.err != nil {
		m.preview.frameErr = msg.err.Error()
		m.preview.frame = ""
	} else {
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
	lo, hi := m.previewRange()
	m.preview.time = clampFloat(m.preview.time, lo, hi)
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
	if !m.preview.playing || msg.gen != m.preview.playGen {
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

// numFilterSteps counts steps's leading run of filter steps (Steps()
// always lists filters before outputs).
func numFilterSteps(steps []pipeline.Step) int {
	n := 0
	for _, s := range steps {
		if !s.Kind().IsFilter() {
			break
		}
		n++
	}
	return n
}

// setTrimBound sets the Trim step's Start (isStart) or End at the current
// preview position, keeping the other bound, and upserting a new Trim
// step (appended after the pipeline's other filter steps) when there is
// none yet. Because a Trim step placed after a Speed step works in
// sped-up time, the input-time preview position is mapped through
// whatever filter steps precede it.
func (m Model) setTrimBound(isStart bool) (Model, tea.Cmd) {
	steps := m.pipeline.Steps()
	before := numFilterSteps(steps)
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
// unaffected by pipeline changes).
func (m Model) maybeRerenderResult() (Model, tea.Cmd) {
	if m.mode != modeMain || !m.preview.resultMode {
		return m, nil
	}
	return m.requestRender()
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
	label := "original"
	if m.preview.resultMode {
		label = "result"
	}

	var content []string
	switch {
	case !m.rendererAvailable:
		content = []string{"install chafa for preview"}
	case m.preview.frameErr != "":
		content = []string{m.preview.frameErr}
	case m.preview.frame != "":
		content = strings.Split(m.preview.frame, "\n")
	}

	lines := []string{boxTop(cols+2, label)}
	for i := 0; i < rows; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, "│"+padOrTruncate(text, cols)+"│")
	}
	lines = append(lines, boxBottom(cols+2))
	return lines
}
