package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// fakeRenderer returns a RenderFunc that records every request it
// receives and answers with text (or err, if non-nil).
func fakeRenderer(text string, err error) (RenderFunc, *[]preview.Request) {
	reqs := &[]preview.Request{}
	fn := func(ctx context.Context, req preview.Request) (string, error) {
		*reqs = append(*reqs, req)
		return text, err
	}
	return fn, reqs
}

func TestFirstWindowSizeIssuesInitialRender(t *testing.T) {
	fn, reqs := fakeRenderer("FRAME", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("first WindowSizeMsg issued no render command")
	}
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	if len(*reqs) != 1 {
		t.Fatalf("renderer called %d times, want 1", len(*reqs))
	}
	req := (*reqs)[0]
	wantCols, wantRows := m.previewBoxSize()
	if req.Time != 0 || req.Filter != "" || req.Cols != wantCols || req.Rows != wantRows {
		t.Errorf("initial request = %+v, want Time 0, Filter \"\", Cols %d, Rows %d", req, wantCols, wantRows)
	}

	out := viewText(m)
	if !strings.Contains(out, "FRAME") {
		t.Errorf("rendered frame missing from the preview box, got:\n%s", out)
	}
}

// step sends msg through Update and, when it yields a command, invokes it
// and feeds the resulting message through Update as well — settling a
// render round-trip so the "at most one render in flight" gate is clear
// for the next step.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	if cmd == nil {
		return m
	}
	mm, _ = m.Update(cmd())
	return mm.(Model)
}

func TestSeekKeysClampToRange(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}) // settle the initial render

	m = step(t, m, key("l"))
	if m.preview.time != 1 {
		t.Errorf("time after l = %v, want 1", m.preview.time)
	}

	m = step(t, m, key("L"))
	if m.preview.time != 6 {
		t.Errorf("time after L = %v, want 6", m.preview.time)
	}

	m = step(t, m, key("h"))
	if m.preview.time != 5 {
		t.Errorf("time after h = %v, want 5", m.preview.time)
	}

	// H repeatedly: clamp at 0.
	for i := 0; i < 3; i++ {
		m = step(t, m, key("H"))
	}
	if m.preview.time != 0 {
		t.Errorf("time after repeated H = %v, want 0 (clamped)", m.preview.time)
	}

	out := viewText(m)
	if !strings.Contains(out, "00:00 / 00:33") {
		t.Errorf("info line did not reflect the seeked position, got:\n%s", out)
	}
	if len(*reqs) == 0 {
		t.Error("seeking issued no render requests")
	}
}

func TestToggleResultMode(t *testing.T) {
	pl := pipeline.New(pipeline.Resolution{Width: 1920, Height: 1080}, pipeline.Speed{Factor: 2}, pipeline.Trim{Start: 1, End: 3})
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pl), WithRenderer(fn, true))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)

	mm, cmd = m.Update(key("v"))
	m = mm.(Model)
	if !m.preview.resultMode {
		t.Fatal("v did not toggle result mode")
	}
	if cmd == nil {
		t.Fatal("v issued no render command")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)

	last := (*reqs)[len(*reqs)-1]
	wantFilter := pipeline.SpatialVideoFilter(pl)
	if last.Filter != wantFilter {
		t.Errorf("result-mode request Filter = %q, want %q", last.Filter, wantFilter)
	}
	lo, hi := pipeline.PlayableRange(m.session.Info, pl)
	if m.preview.time < lo || m.preview.time > hi {
		t.Errorf("time %v not clamped into playable range [%v,%v]", m.preview.time, lo, hi)
	}

	out := viewText(m)
	if !strings.Contains(out, "result") {
		t.Errorf("preview label did not read 'result', got:\n%s", out)
	}

	mm, cmd = m.Update(key("v"))
	m = mm.(Model)
	if m.preview.resultMode {
		t.Error("second v did not toggle back to original")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	out = viewText(m)
	if !strings.Contains(out, "original") {
		t.Errorf("preview label did not read 'original', got:\n%s", out)
	}
}

func TestClampRenderTimeStaysOneFrameBeforeRangeEnd(t *testing.T) {
	cases := []struct{ t, lo, hi, fps, want float64 }{
		{33, 0, 33, 30, 33 - 1.0/30},
		{5, 0, 33, 30, 5},            // well inside the range: unaffected
		{-1, 0, 33, 30, 0},           // below lo: clamped up to lo
		{100, 0, 33, 0, 33 - 1.0/30}, // fps <= 0 falls back to 30
	}
	for _, c := range cases {
		if got := clampRenderTime(c.t, c.lo, c.hi, c.fps); got != c.want {
			t.Errorf("clampRenderTime(%v,%v,%v,%v) = %v, want %v", c.t, c.lo, c.hi, c.fps, got, c.want)
		}
	}
}

func TestPreviewRequestClampsTimeAtRangeEnd(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.preview.time = m.session.Info.Duration // seek to exactly the clip's end

	_, cmd := m.requestRender()
	if cmd == nil {
		t.Fatal("requestRender issued no command")
	}
	cmd()

	last := (*reqs)[len(*reqs)-1]
	fps := m.session.Info.Video.FPS
	want := m.session.Info.Duration - 1/fps
	if last.Time != want {
		t.Errorf("render request Time at the range end = %v, want %v (one frame before the end)", last.Time, want)
	}
	argv := preview.FrameArgs(last)
	wantSS := units.FormatNumber(want)
	found := false
	for i, a := range argv {
		if a == "-ss" {
			found = true
			if argv[i+1] != wantSS {
				t.Errorf("-ss = %q, want %q", argv[i+1], wantSS)
			}
		}
	}
	if !found {
		t.Fatal("-ss not present in FrameArgs argv")
	}
}

func TestTogglingPlayOffThenOnDropsTheStaleTickChain(t *testing.T) {
	fn, _ := fakeRenderer("F", nil)
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})

	mm, c1 := m.Update(key("space")) // play on: chain A starts
	m = mm.(Model)
	mm, _ = m.Update(key("space")) // pause: chain A is now stale
	m = mm.(Model)
	mm, c3 := m.Update(key("space")) // play on again: chain B starts
	m = mm.(Model)

	if c1 == nil || c3 == nil {
		t.Fatal("space did not issue a tick command")
	}

	// The stale chain-A tick must be dropped without re-arming.
	staleMsg := c1()
	mm, staleCmd := m.Update(staleMsg)
	m = mm.(Model)
	if staleCmd != nil {
		t.Fatal("a stale tick from a superseded play session re-armed")
	}

	// The current chain-B tick advances playback and re-arms normally.
	msg := c3()
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("the current tick chain did not re-arm")
	}
	if m.preview.time <= 0 {
		t.Errorf("current tick chain did not advance playback, time = %v", m.preview.time)
	}
}

func TestSpacePlayAdvancesAndStopsAtRangeEnd(t *testing.T) {
	fn, _ := fakeRenderer("F", nil)
	pl := pipeline.New(pipeline.Trim{Start: 32.8, End: 33})
	m := New(testSession(pl), WithRenderer(fn, true))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Switch to result mode so the playable range is the short Trim
	// window [32.8, 33], making it easy to reach the end quickly.
	m = step(t, m, key("v"))
	m.preview.time = 32.8

	mm, cmd := m.Update(key("space"))
	m = mm.(Model)
	if !m.preview.playing {
		t.Fatal("space did not start playback")
	}
	if cmd == nil {
		t.Fatal("space issued no tick command")
	}

	// Drive the tick stream directly through Update, the documented seam
	// for the preview's internal messages; the exact Cmd a tick returns
	// (a lone re-arm, or a batch that also requests a render) is an
	// implementation detail this test does not need to unpack.
	for i := 0; i < 5 && m.preview.playing; i++ {
		mm, _ = m.Update(previewTickMsg{})
		m = mm.(Model)
	}

	if m.preview.playing {
		t.Error("playback did not stop at the range end")
	}
	if m.preview.time != 33 {
		t.Errorf("final time = %v, want 33 (clamped at range end)", m.preview.time)
	}
}

func TestAtMostOneRenderInFlightDirtyReRenders(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	if len(*reqs) != 0 {
		t.Fatalf("render fired before its Cmd was invoked: %d requests", len(*reqs))
	}

	// A seek while the initial render is still "in flight" (its Cmd has
	// not been invoked) must not start a second one.
	mm, seekCmd := m.Update(key("l"))
	m = mm.(Model)
	if seekCmd != nil {
		t.Error("seeking during an in-flight render issued a new render command")
	}
	if !m.preview.dirty {
		t.Error("seeking during an in-flight render did not mark it dirty")
	}

	// Land the in-flight render: it should immediately re-render once,
	// picking up the seeked time.
	msg := cmd()
	mm, cmd = m.Update(msg)
	m = mm.(Model)
	if len(*reqs) != 1 {
		t.Fatalf("requests so far = %d, want 1", len(*reqs))
	}
	if cmd == nil {
		t.Fatal("dirty flag did not trigger a follow-up render")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if len(*reqs) != 2 {
		t.Fatalf("requests after the follow-up render = %d, want 2", len(*reqs))
	}
	if (*reqs)[1].Time != m.preview.time {
		t.Errorf("follow-up request Time = %v, want the seeked time %v", (*reqs)[1].Time, m.preview.time)
	}
	if m.preview.dirty {
		t.Error("dirty flag was not cleared")
	}
}

func TestStaleFrameDropped(t *testing.T) {
	fn, _ := fakeRenderer("F", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))
	m = resized(m, 100, 30)
	m.preview.pendingSeq = 5
	m.preview.frame = "existing"

	mm, cmd := m.Update(previewFrameMsg{seq: 3, text: "STALE"})
	m = mm.(Model)
	if cmd != nil {
		t.Error("a stale frame issued a command")
	}
	if m.preview.frame != "existing" {
		t.Errorf("a stale frame overwrote the current one: %q", m.preview.frame)
	}
	if m.preview.pendingSeq != 5 {
		t.Errorf("pendingSeq changed on a stale frame: %d", m.preview.pendingSeq)
	}
}

func TestRendererUnavailableShowsHintAndSkipsRenders(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, false))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	if cmd != nil {
		t.Error("resize issued a render command while the renderer is unavailable")
	}
	if len(*reqs) != 0 {
		t.Errorf("renderer called %d times, want 0", len(*reqs))
	}
	out := viewText(m)
	if !strings.Contains(out, "install chafa for preview") {
		t.Errorf("missing the chafa hint, got:\n%s", out)
	}

	// Every other key still works.
	mm, _ = m.Update(key("tab"))
	m = mm.(Model)
	if m.focus != focusPipeline {
		t.Error("tab did not work while the renderer is unavailable")
	}
}

func TestRenderErrorShowsOneLine(t *testing.T) {
	fn, _ := fakeRenderer("", errors.New("ffmpeg: boom"))
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)

	if m.preview.frameErr == "" {
		t.Fatal("expected a frame error to be recorded")
	}
	out := viewText(m)
	if !strings.Contains(out, "ffmpeg: boom") {
		t.Errorf("missing the render error in the preview box, got:\n%s", out)
	}
}

func TestMultiLineRenderErrorShowsItsLastLineOnOneRow(t *testing.T) {
	fn, _ := fakeRenderer("", errors.New("ffmpeg: exit status 1: noise\nmore noise\n  clip.mov: No such file or directory  \n\n"))
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})

	out := viewText(m)
	if !strings.Contains(out, "│clip.mov: No such file or directory ") {
		t.Errorf("preview box does not show the error's last line, trimmed, on one row:\n%s", out)
	}
	if strings.Contains(out, "noise") {
		t.Errorf("preview box kept the error's earlier lines:\n%s", out)
	}
	if h := strings.Count(out, "\n") + 1; h > 30 {
		t.Errorf("view is %d lines tall in a 30-line terminal", h)
	}
}

func TestResizeRequestsFreshRender(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true))

	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if len(*reqs) != 1 {
		t.Fatalf("requests after the first resize = %d, want 1", len(*reqs))
	}

	mm, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("a resize issued no render command")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if len(*reqs) != 2 {
		t.Fatalf("requests after the second resize = %d, want 2", len(*reqs))
	}
	wantCols, wantRows := m.previewBoxSize()
	last := (*reqs)[1]
	if last.Cols != wantCols || last.Rows != wantRows {
		t.Errorf("resized request = %+v, want Cols %d Rows %d", last, wantCols, wantRows)
	}
}

func TestPreviewRerendersWhenAnAddedStepShrinksTheBox(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	pl := pipeline.New(pipeline.Speed{Factor: 2}, pipeline.FrameRate{FPS: 30})
	m := step(t, New(testSession(pl), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	_, rowsBefore := m.previewBoxSize()

	m = openMenu(t, m, pipeline.KindResolution)
	m = step(t, m, key("enter"))

	_, rows := m.previewBoxSize()
	if rows >= rowsBefore {
		t.Fatalf("adding a third step did not shrink the preview box (%d -> %d rows)", rowsBefore, rows)
	}
	if last := (*reqs)[len(*reqs)-1]; last.Rows != rows {
		t.Errorf("preview box is %d rows but the latest render was for %d rows", rows, last.Rows)
	}
}

func TestPreviewRerendersWhenTheExpandedCommandShrinksTheBox(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	long := strings.Repeat("y", 300)
	pl := pipeline.New(pipeline.RawArgs{Text: long, Args: []string{long}})
	m := step(t, New(testSession(pl), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	_, rowsBefore := m.previewBoxSize()

	m = step(t, m, key("c"))

	_, rows := m.previewBoxSize()
	if rows >= rowsBefore {
		t.Fatalf("expanding the command did not shrink the preview box (%d -> %d rows)", rowsBefore, rows)
	}
	if last := (*reqs)[len(*reqs)-1]; last.Rows != rows {
		t.Errorf("preview box is %d rows but the latest render was for %d rows", rows, last.Rows)
	}
}

func TestPreviewDoesNotRerenderWhenTheBoxSizeIsUnchanged(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	before := len(*reqs)

	m = step(t, m, key("tab"))
	m = step(t, m, key("j"))

	if len(*reqs) != before {
		t.Errorf("keys that leave the preview box alone issued %d renders", len(*reqs)-before)
	}
}

// TestResultModeRerenderClampsPreviewTimeIntoNewRange covers a pipeline
// edit in result mode that shrinks the playable range out from under the
// preview's current position: the follow-up render must land inside the
// new range, not at the stale position.
func TestResultModeRerenderClampsPreviewTimeIntoNewRange(t *testing.T) {
	fn, reqs := fakeRenderer("F", nil)
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("v"))
	m.preview.time = 30
	m = openMenu(t, m, pipeline.KindTrim)
	m = typeText(m, "1-3")
	m = step(t, m, key("enter"))

	last := (*reqs)[len(*reqs)-1]
	if last.Time < 1 || last.Time > 3 {
		t.Fatalf("result preview rendered at %vs, want inside the new [1,3] range", last.Time)
	}
}

func TestResultModeEditClampsThePreviewPositionItself(t *testing.T) {
	fn, _ := fakeRenderer("F", nil)
	m := step(t, New(testSession(pipeline.New()), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("v"))
	for i := 0; i < 6; i++ {
		m = step(t, m, key("L"))
	}
	if m.preview.time != 30 {
		t.Fatalf("setup: position = %vs, want 30s", m.preview.time)
	}

	m = openMenu(t, m, pipeline.KindTrim)
	m = typeText(m, "1-3")
	m = step(t, m, key("enter"))

	if m.preview.time != 3 {
		t.Errorf("position after trimming to 1-3 = %vs, want 3s (clamped to the range end)", m.preview.time)
	}
	if out := viewText(m); !strings.Contains(out, "00:03 / 00:33") {
		t.Errorf("info line does not show the clamped position:\n%s", out)
	}

	m = step(t, m, key("o"))
	tr, _ := m.pipeline.Find(pipeline.KindTrim)
	if got := tr.(pipeline.Trim); got.Start != 1 || got.End != 3 {
		t.Errorf("o after the range shrank set Trim to %+v, want {1,3}", got)
	}
}

func TestResultModeUndoClampsThePreviewPosition(t *testing.T) {
	fn, _ := fakeRenderer("F", nil)
	pl := pipeline.New(pipeline.Trim{Start: 1, End: 3})
	m := step(t, New(testSession(pl), WithRenderer(fn, true)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("v"))
	m = openMenu(t, m, pipeline.KindTrim)
	m.modal.input.SetValue("1-30")
	m = step(t, m, key("enter"))
	for i := 0; i < 6; i++ {
		m = step(t, m, key("L"))
	}
	if m.preview.time <= 3 {
		t.Fatalf("setup: position = %vs, want past 3s", m.preview.time)
	}

	m = step(t, m, key("tab"))
	m = step(t, m, key("u"))

	if m.preview.time != 3 {
		t.Errorf("position after undoing back to a 1-3 trim = %vs, want 3s", m.preview.time)
	}
}

func TestPipelineChangeInResultModeRerenders(t *testing.T) {
	pl := pipeline.New(pipeline.FrameRate{FPS: 30})
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pl), WithRenderer(fn, true))
	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)

	mm, cmd = m.Update(key("v")) // enter result mode
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	before := len(*reqs)

	m.focus = focusPipeline
	m.pipelineCursor = 0
	mm, cmd = m.Update(key("x")) // remove the FrameRate step
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("a pipeline edit in result mode issued no render command")
	}
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if len(*reqs) != before+1 {
		t.Errorf("requests after the edit = %d, want %d", len(*reqs), before+1)
	}
}

func TestPipelineChangeInOriginalModeDoesNotRerender(t *testing.T) {
	pl := pipeline.New(pipeline.FrameRate{FPS: 30})
	fn, reqs := fakeRenderer("F", nil)
	m := New(testSession(pl), WithRenderer(fn, true))
	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	before := len(*reqs)

	m.focus = focusPipeline
	m.pipelineCursor = 0
	mm, cmd = m.Update(key("x"))
	m = mm.(Model)
	if cmd != nil {
		t.Error("a pipeline edit in original mode issued a render command")
	}
	if len(*reqs) != before {
		t.Errorf("requests after the edit = %d, want %d (unchanged)", len(*reqs), before)
	}
}

func TestColorProfileMapping(t *testing.T) {
	m := New(testSession(pipeline.New()))
	if m.colorProfile != "256" {
		t.Fatalf("default colorProfile = %q, want \"256\"", m.colorProfile)
	}

	cases := map[colorprofile.Profile]string{
		colorprofile.TrueColor: "full",
		colorprofile.ANSI256:   "256",
		colorprofile.ANSI:      "16",
		colorprofile.Ascii:     "none",
		colorprofile.NoTTY:     "none",
	}
	for profile, want := range cases {
		mm, _ := m.Update(tea.ColorProfileMsg{Profile: profile})
		got := mm.(Model).colorProfile
		if got != want {
			t.Errorf("colorProfileString(%v) = %q, want %q", profile, got, want)
		}
	}
}

func TestSetTrimBoundsAtPreviewPosition(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m.preview.time = 5

	mm, _ := m.Update(key("i"))
	m = mm.(Model)
	tr, ok := m.pipeline.Find(pipeline.KindTrim)
	if !ok || tr.(pipeline.Trim).Start != 5 {
		t.Fatalf("Trim after i = %v, want Start 5", tr)
	}

	m.preview.time = 20
	mm, _ = m.Update(key("o"))
	m = mm.(Model)
	tr, ok = m.pipeline.Find(pipeline.KindTrim)
	trim := tr.(pipeline.Trim)
	if !ok || trim.Start != 5 || trim.End != 20 {
		t.Fatalf("Trim after o = %v, want Start 5 End 20", trim)
	}
}

func TestSetTrimBoundMapsThroughPrecedingSpeed(t *testing.T) {
	pl := pipeline.New(pipeline.Speed{Factor: 2})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m.preview.time = 10 // input-time position

	mm, _ := m.Update(key("i"))
	m = mm.(Model)
	tr, ok := m.pipeline.Find(pipeline.KindTrim)
	// MapTime maps the input-time position through the preceding Speed(2)
	// step (Trim is appended after it): 10 input-seconds -> 5 chain-local.
	if !ok || tr.(pipeline.Trim).Start != 5 {
		t.Fatalf("Trim = %v, want Start 5 (10 input-seconds through Speed 2x)", tr)
	}
}

func TestSetTrimBoundIsUndoable(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m.preview.time = 5

	mm, _ := m.Update(key("i"))
	m = mm.(Model)
	if _, ok := m.pipeline.Find(pipeline.KindTrim); !ok {
		t.Fatal("i did not add a Trim step")
	}

	m.focus = focusPipeline
	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	if _, ok := m.pipeline.Find(pipeline.KindTrim); ok {
		t.Error("u did not undo the i-driven Trim change")
	}
}
