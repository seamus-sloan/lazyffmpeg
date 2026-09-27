package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

// key builds a tea.KeyPressMsg whose String() equals s, for the small set of
// keys lazyff's TUI recognizes.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(s)
		return tea.KeyPressMsg{Code: r[0], Text: s}
	}
}

func testInfo() probe.Info {
	return probe.Info{
		Duration:  33.4,
		SizeBytes: 276_100_000,
		BitRate:   60_000_000,
		Video:     probe.VideoStream{Codec: "hevc", Width: 3652, Height: 2560, FPS: 60},
	}
}

func testSession(pl pipeline.Pipeline) app.Session {
	return app.Session{
		Input:    "clip.mov",
		Info:     testInfo(),
		Pipeline: pl,
	}
}

func viewText(m Model) string {
	return ansi.Strip(m.View().Content)
}

func resized(m Model, w, h int) Model {
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return mm.(Model)
}

func TestViewShowsFrameTitleAndInfoLine(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	out := viewText(m)

	if !strings.Contains(out, "lazyff · clip.mov") {
		t.Errorf("view missing frame title, got:\n%s", out)
	}
	wantInfo := "00:00 / 00:33  3652×2560 · H.265 · 276.1 MB"
	if !strings.Contains(out, wantInfo) {
		t.Errorf("view missing info line %q, got:\n%s", wantInfo, out)
	}
}

func TestFooterShowsKeyHints(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	out := viewText(m)
	want := "r run · tab focus · ? help · q quit"
	if !strings.Contains(out, want) {
		t.Errorf("view missing footer hints %q, got:\n%s", want, out)
	}
}

func TestViewShowsPipelineAndMenu(t *testing.T) {
	pl := pipeline.New(pipeline.FrameRate{FPS: 30})
	m := New(testSession(pl))
	m = resized(m, 100, 30)

	out := viewText(m)

	if !strings.Contains(out, "PIPELINE") {
		t.Errorf("view missing PIPELINE heading, got:\n%s", out)
	}
	wantStep := "1. Frame rate   30 fps"
	if !strings.Contains(out, wantStep) {
		t.Errorf("view missing pipeline step %q, got:\n%s", wantStep, out)
	}

	for _, want := range []string{
		"MENU", "Video", "Output", "Run",
		"Resolution", "Speed", "Trim", "Frame rate",
		"Encoder", "Quality / target size", "Audio", "Container", "Raw args",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing menu text %q, got:\n%s", want, out)
		}
	}
}

// allKindSteps holds one step of every kind, in Kind order, so a subset of
// the first n gives a pipeline of exactly n distinct steps.
func allKindSteps() []pipeline.Step {
	return []pipeline.Step{
		pipeline.Resolution{Width: 1920, Height: 1080},
		pipeline.Speed{Factor: 2},
		pipeline.Trim{Start: 1, End: 3},
		pipeline.FrameRate{FPS: 30},
		pipeline.Encoder{Codec: pipeline.CodecH265},
		pipeline.Quality{CRF: 20},
		pipeline.Audio{Mode: pipeline.AudioAAC, BitrateK: 128},
		pipeline.Container{Format: pipeline.FormatMKV},
		pipeline.RawArgs{Text: "-map_metadata -1", Args: []string{"-map_metadata", "-1"}},
	}
}

func TestMainFrameFitsTerminalHeight(t *testing.T) {
	all := allKindSteps()
	for _, n := range []int{3, 6, 9} {
		pl := pipeline.New(all[:n]...)
		m := resized(New(testSession(pl)), 100, 30)

		out := viewText(m)
		if h := strings.Count(out, "\n") + 1; h > 30 {
			t.Errorf("%d steps: view is %d lines tall in a 30-line terminal, want <= 30:\n%s", n, h, out)
		}
		if !strings.Contains(out, "→ "+m.session.OutputPath(m.pipeline)) {
			t.Errorf("%d steps: footer output-path line missing, got:\n%s", n, out)
		}
		if !strings.HasSuffix(strings.TrimRight(out, "\n"), "╯") {
			t.Errorf("%d steps: bottom border missing, got:\n%s", n, out)
		}

		mm, _ := m.Update(key("c"))
		expanded := mm.(Model)
		outExpanded := viewText(expanded)
		if h := strings.Count(outExpanded, "\n") + 1; h > 30 {
			t.Errorf("%d steps, c expanded: view is %d lines tall, want <= 30:\n%s", n, h, outExpanded)
		}
	}
}

func TestFullCommandFullyShownAcrossFooterLinesWhenExpanded(t *testing.T) {
	long := strings.Repeat("y", 150)
	pl := pipeline.New(pipeline.RawArgs{Text: long, Args: []string{long}})
	m := resized(New(testSession(pl)), 100, 30)

	mm, _ := m.Update(key("c"))
	m = mm.(Model)

	commandLines, _ := m.footerLines()
	if !strings.Contains(strings.Join(commandLines, ""), long) {
		t.Fatalf("full command not fully present across the wrapped footer lines: %v", commandLines)
	}

	out := viewText(m)
	if h := strings.Count(out, "\n") + 1; h > 30 {
		t.Errorf("view is %d lines tall in a 30-line terminal after c, want <= 30:\n%s", h, out)
	}
}

func TestPipelineWindowKeepsCursorVisible(t *testing.T) {
	cases := []struct{ stepCount, visible, cursor, wantStart, wantEnd int }{
		{9, 9, 4, 0, 9}, // fits entirely: no scroll
		{9, 3, 0, 0, 3}, // cursor at the top
		{9, 3, 8, 6, 9}, // cursor at the bottom
		{9, 3, 4, 3, 6}, // cursor centered
	}
	for _, c := range cases {
		start, end := pipelineWindow(c.stepCount, c.visible, c.cursor)
		if start != c.wantStart || end != c.wantEnd {
			t.Errorf("pipelineWindow(%d,%d,%d) = (%d,%d), want (%d,%d)",
				c.stepCount, c.visible, c.cursor, start, end, c.wantStart, c.wantEnd)
		}
		if c.cursor < start || c.cursor >= end {
			t.Errorf("pipelineWindow(%d,%d,%d) = (%d,%d) excludes the cursor",
				c.stepCount, c.visible, c.cursor, start, end)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 60, 20)

	out := viewText(m)
	want := "Terminal too small: need 80×24, have 60×20"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestViewResizeRestoresLayout(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 60, 20)
	m = resized(m, 100, 30)

	out := viewText(m)
	if strings.Contains(out, "Terminal too small") {
		t.Errorf("resize did not restore layout:\n%s", out)
	}
	if !strings.Contains(out, "lazyff · clip.mov") {
		t.Errorf("resize did not restore frame title:\n%s", out)
	}
}

func TestTabTogglesFocus(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	if m.focus != focusMenu {
		t.Fatalf("initial focus = %v, want focusMenu", m.focus)
	}
	mm, _ := m.Update(key("tab"))
	m = mm.(Model)
	if m.focus != focusPipeline {
		t.Errorf("after tab, focus = %v, want focusPipeline", m.focus)
	}
	mm, _ = m.Update(key("tab"))
	m = mm.(Model)
	if m.focus != focusMenu {
		t.Errorf("after second tab, focus = %v, want focusMenu", m.focus)
	}
}

func TestMenuCursorMovesAndClamps(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	mm, _ := m.Update(key("k")) // up at top: clamps to 0
	m = mm.(Model)
	if m.menuCursor != 0 {
		t.Fatalf("menuCursor = %d, want 0", m.menuCursor)
	}

	for i := 0; i < 20; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	if m.menuCursor != menuRunIndex {
		t.Errorf("menuCursor = %d, want clamped to %d", m.menuCursor, menuRunIndex)
	}
}

func TestPipelineRemoveMoveUndo(t *testing.T) {
	pl := pipeline.New(pipeline.Speed{Factor: 2}, pipeline.Trim{Start: 1, End: 3})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	mm, _ := m.Update(key("tab"))
	m = mm.(Model)

	if m.focus != focusPipeline {
		t.Fatalf("focus = %v, want focusPipeline", m.focus)
	}
	if m.pipeline.Len() != 2 {
		t.Fatalf("pipeline.Len() = %d, want 2", m.pipeline.Len())
	}

	// J moves the first (Speed) step down past Trim.
	mm, _ = m.Update(key("J"))
	m = mm.(Model)
	steps := m.pipeline.Steps()
	if steps[0].Kind() != pipeline.KindTrim || steps[1].Kind() != pipeline.KindSpeed {
		t.Fatalf("after J, order = %v, %v", steps[0].Kind(), steps[1].Kind())
	}
	if len(m.undo) != 1 {
		t.Fatalf("undo depth after J = %d, want 1", len(m.undo))
	}

	// Move the cursor back to index 0 (Trim) and press K: there is no
	// adjacent filter step above it, so this is a no-op with no undo entry.
	mm, _ = m.Update(key("k"))
	m = mm.(Model)
	if m.pipelineCursor != 0 {
		t.Fatalf("pipelineCursor = %d, want 0", m.pipelineCursor)
	}
	mm, _ = m.Update(key("K"))
	m = mm.(Model)
	if len(m.undo) != 1 {
		t.Errorf("undo depth after no-op K = %d, want 1 (unchanged)", len(m.undo))
	}

	// x removes the step under the cursor (Trim, at index 0).
	mm, _ = m.Update(key("x"))
	m = mm.(Model)
	if m.pipeline.Len() != 1 {
		t.Fatalf("pipeline.Len() after x = %d, want 1", m.pipeline.Len())
	}
	if _, ok := m.pipeline.Find(pipeline.KindTrim); ok {
		t.Errorf("Trim step still present after x")
	}
	if len(m.undo) != 2 {
		t.Fatalf("undo depth after x = %d, want 2", len(m.undo))
	}
	if m.pipelineCursor != 0 {
		t.Errorf("pipelineCursor after removing last-but-one item = %d, want 0", m.pipelineCursor)
	}

	// u undoes the remove.
	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	if m.pipeline.Len() != 2 {
		t.Fatalf("pipeline.Len() after u = %d, want 2", m.pipeline.Len())
	}

	// u undoes the move.
	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	steps = m.pipeline.Steps()
	if steps[0].Kind() != pipeline.KindSpeed || steps[1].Kind() != pipeline.KindTrim {
		t.Fatalf("after second u, order = %v, %v", steps[0].Kind(), steps[1].Kind())
	}

	// u with an empty undo stack is a no-op.
	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	if m.pipeline.Len() != 2 {
		t.Errorf("pipeline.Len() after no-op u = %d, want 2", m.pipeline.Len())
	}
}

func TestFooterTruncatesAndTogglesFull(t *testing.T) {
	pl := pipeline.New(pipeline.RawArgs{
		Text: strings.Repeat("x", 40),
		Args: []string{strings.Repeat("x", 40)},
	})
	m := New(testSession(pl))
	m = resized(m, 100, 30)

	out := viewText(m)
	if !strings.Contains(out, "…") {
		t.Errorf("expected footer command to be truncated with an ellipsis, got:\n%s", out)
	}

	mm, _ := m.Update(key("c"))
	m = mm.(Model)
	full := viewText(m)
	if strings.Contains(full, "…") {
		t.Errorf("expected the full command after 'c', got:\n%s", full)
	}
}

func TestFooterShowsCompileError(t *testing.T) {
	pl := pipeline.New(pipeline.Trim{Start: 40}) // starts past the 33s duration
	m := New(testSession(pl))
	m = resized(m, 100, 30)

	out := viewText(m)
	if !strings.Contains(out, "trim is outside the clip") {
		t.Errorf("expected compile error in footer, got:\n%s", out)
	}
}

func TestHelpOverlayOpensAndAnyKeyCloses(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	mm, _ := m.Update(key("?"))
	m = mm.(Model)
	if !m.showHelp {
		t.Fatalf("? did not open help")
	}
	out := viewText(m)
	if !strings.Contains(out, "tab") {
		t.Errorf("help overlay missing key hints, got:\n%s", out)
	}

	mm, _ = m.Update(key("z"))
	m = mm.(Model)
	if m.showHelp {
		t.Errorf("help overlay did not close on key press")
	}
}

func TestQuitReturnsQuitCmd(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)

	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestAltScreenAndPipelineAccessor(t *testing.T) {
	pl := pipeline.New(pipeline.FrameRate{FPS: 24})
	m := New(testSession(pl))
	m = resized(m, 100, 30)

	if !m.View().AltScreen {
		t.Error("expected AltScreen to be true")
	}
	if got, ok := m.Pipeline().Find(pipeline.KindFrameRate); !ok || got.(pipeline.FrameRate).FPS != 24 {
		t.Errorf("Pipeline() = %v, want FrameRate 24", m.Pipeline())
	}
}
