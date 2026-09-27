package tui

import (
	"strings"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
)

func openMenu(t *testing.T, m Model, kind pipeline.Kind) Model {
	t.Helper()
	for i, k := range menuKinds {
		if k == kind {
			m.menuCursor = i
			break
		}
	}
	m.focus = focusMenu
	mm, _ := m.Update(key("enter"))
	return mm.(Model)
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		mm, _ := m.Update(key(string(r)))
		m = mm.(Model)
	}
	return m
}

func TestMenuEnterOpensModal(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindSpeed)

	if m.modal == nil {
		t.Fatal("enter on a MENU item did not open a modal")
	}
	if m.modal.kind != pipeline.KindSpeed {
		t.Errorf("modal.kind = %v, want KindSpeed", m.modal.kind)
	}

	out := viewText(m)
	if !strings.Contains(out, "Speed") {
		t.Errorf("modal missing kind label, got:\n%s", out)
	}
	if !strings.Contains(out, "2x") || !strings.Contains(out, "Custom…") {
		t.Errorf("modal missing presets, got:\n%s", out)
	}
	if !strings.Contains(out, "lazyff · clip.mov") {
		t.Errorf("frame title not visible under the modal, got:\n%s", out)
	}
}

func TestModalPresetSelectUpsertsAndCloses(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindSpeed)

	// Cursor starts on the first preset (0.5x); move to 2x (index 2).
	mm, _ := m.Update(key("down"))
	m = mm.(Model)
	mm, _ = m.Update(key("down"))
	m = mm.(Model)

	mm, _ = m.Update(key("enter"))
	m = mm.(Model)

	if m.modal != nil {
		t.Fatal("modal did not close after selecting a preset")
	}
	s, ok := m.pipeline.Find(pipeline.KindSpeed)
	if !ok || s.(pipeline.Speed).Factor != 2 {
		t.Errorf("pipeline Speed = %v, want Factor 2", s)
	}
}

func TestModalReselectSameKindReplacesInPlace(t *testing.T) {
	pl := pipeline.New(pipeline.Trim{Start: 1, End: 3})
	m := New(testSession(pl))
	m = resized(m, 100, 30)

	// Add Speed 2x after Trim.
	m = openMenu(t, m, pipeline.KindSpeed)
	for i := 0; i < 2; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	steps := m.pipeline.Steps()
	if len(steps) != 2 || steps[1].Kind() != pipeline.KindSpeed {
		t.Fatalf("expected [Trim, Speed], got %v", steps)
	}

	// Reopen Speed: it preselects the current 2x preset (index 2). Move to
	// 3x (index 3) and confirm.
	m = openMenu(t, m, pipeline.KindSpeed)
	if m.modal.cursor != 2 {
		t.Fatalf("reopened Speed modal cursor = %d, want 2 (preselected 2x)", m.modal.cursor)
	}
	mm, _ = m.Update(key("down"))
	m = mm.(Model)
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)

	steps = m.pipeline.Steps()
	if len(steps) != 2 {
		t.Fatalf("expected exactly one Speed step, got %v", steps)
	}
	if steps[1].Kind() != pipeline.KindSpeed || steps[1].(pipeline.Speed).Factor != 3 {
		t.Errorf("expected Speed 3x at position 1, got %v", steps[1])
	}
}

func TestModalUpDownAlwaysMove(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindFrameRate)

	start := m.modal.cursor
	mm, _ := m.Update(key("down"))
	m = mm.(Model)
	if m.modal.cursor != start+1 {
		t.Errorf("down did not move cursor: %d -> %d", start, m.modal.cursor)
	}
	mm, _ = m.Update(key("up"))
	m = mm.(Model)
	if m.modal.cursor != start {
		t.Errorf("up did not move cursor back to %d, got %d", start, m.modal.cursor)
	}
}

func TestModalMovingOntoCustomFocusesInput(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindFrameRate)

	// FrameRate presets: 60,30,24,15,10,Custom… — Custom… is index 5.
	for i := 0; i < 5; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	if !isCustomOption(m.modal.options, m.modal.cursor) {
		t.Fatalf("cursor is not on Custom…: %d", m.modal.cursor)
	}
	if !m.modal.input.Focused() {
		t.Error("Custom… text input is not focused")
	}

	// j/k now type into the input rather than moving the cursor.
	mm, _ := m.Update(key("j"))
	m = mm.(Model)
	if m.modal.input.Value() != "j" {
		t.Errorf("expected 'j' typed into the input, got %q", m.modal.input.Value())
	}
}

func TestModalCustomValidUpsertsAndCloses(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindFrameRate)
	for i := 0; i < 5; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	m = typeText(m, "23.5")

	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	if m.modal != nil {
		t.Fatal("modal did not close after a valid custom value")
	}
	s, ok := m.pipeline.Find(pipeline.KindFrameRate)
	if !ok || s.(pipeline.FrameRate).FPS != 23.5 {
		t.Errorf("pipeline FrameRate = %v, want FPS 23.5", s)
	}
}

func TestModalCustomOddWidthShowsTheEvenWidthUsed(t *testing.T) {
	m := resized(New(testSession(pipeline.New())), 100, 30)
	m = openMenu(t, m, pipeline.KindResolution)
	for i := 0; i < 6; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	m = typeText(m, "853x")
	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	if out := viewText(m); !strings.Contains(out, "1. Resolution   852×auto") {
		t.Errorf("PIPELINE does not show the even width the encode uses:\n%s", out)
	}
}

func TestModalCustomInvalidShowsErrorKeepsPipeline(t *testing.T) {
	pl := pipeline.New(pipeline.FrameRate{FPS: 30})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindFrameRate)
	for i := 0; i < 5; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	m = typeText(m, "notanumber")

	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	if m.modal == nil {
		t.Fatal("modal closed despite an invalid custom value")
	}
	if m.modal.err == "" {
		t.Error("expected an error message under the input")
	}
	s, _ := m.pipeline.Find(pipeline.KindFrameRate)
	if s.(pipeline.FrameRate).FPS != 30 {
		t.Errorf("pipeline changed despite an invalid custom value: %v", s)
	}
}

func TestModalEscClosesNoChangeNoUndo(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindSpeed)

	mm, _ := m.Update(key("down"))
	m = mm.(Model)

	mm, _ = m.Update(key("esc"))
	m = mm.(Model)

	if m.modal != nil {
		t.Fatal("esc did not close the modal")
	}
	if _, ok := m.pipeline.Find(pipeline.KindSpeed); ok {
		t.Error("esc applied a change to the pipeline")
	}
	if len(m.undo) != 0 {
		t.Errorf("esc added an undo entry: depth %d, want 0", len(m.undo))
	}
}

func TestModalChangeIsUndoable(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindSpeed)
	mm, _ := m.Update(key("down"))
	m = mm.(Model)
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)

	if _, ok := m.pipeline.Find(pipeline.KindSpeed); !ok {
		t.Fatal("Speed step was not added")
	}

	m.focus = focusPipeline
	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	if _, ok := m.pipeline.Find(pipeline.KindSpeed); ok {
		t.Error("u did not undo the modal-driven change")
	}
}

func TestPipelineEnterReopensWithPreselectedPreset(t *testing.T) {
	pl := pipeline.New(pipeline.Speed{Factor: 2})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m.focus = focusPipeline
	m.pipelineCursor = 0

	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	if m.modal == nil || m.modal.kind != pipeline.KindSpeed {
		t.Fatalf("PIPELINE enter did not reopen the Speed modal: %v", m.modal)
	}
	got := m.modal.options[m.modal.cursor]
	if got.step == nil || got.step.(pipeline.Speed).Factor != 2 {
		t.Errorf("preselected option = %v, want the current Speed 2x preset", got)
	}
}

func TestPipelineEnterReopensCustomPrefilled(t *testing.T) {
	pl := pipeline.New(pipeline.Trim{Start: 5, End: 20})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m.focus = focusPipeline
	m.pipelineCursor = 0

	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	if m.modal == nil {
		t.Fatal("PIPELINE enter did not reopen the Trim modal")
	}
	if !isCustomOption(m.modal.options, m.modal.cursor) {
		t.Fatalf("Trim modal did not preselect Custom…: cursor %d", m.modal.cursor)
	}
	if m.modal.input.Value() != "5-20" {
		t.Errorf("Custom… prefill = %q, want %q", m.modal.input.Value(), "5-20")
	}
}

func TestContainerMkvChangesFooterOutputPath(t *testing.T) {
	m := New(testSession(pipeline.New()))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindContainer)

	// Container presets: mp4, mov, mkv, webm.
	for i := 0; i < 2; i++ {
		mm, _ := m.Update(key("down"))
		m = mm.(Model)
	}
	mm, _ := m.Update(key("enter"))
	m = mm.(Model)

	out := viewText(m)
	if !strings.Contains(out, "(edited).mkv") {
		t.Errorf("footer output path did not reflect the mkv container, got:\n%s", out)
	}
}

func TestQualityModalDoesNotOpenForCopyEncoder(t *testing.T) {
	pl := pipeline.New(pipeline.Encoder{Codec: pipeline.CodecCopy})
	m := resized(New(testSession(pl)), 100, 30)
	m = openMenu(t, m, pipeline.KindQuality)
	if m.modal != nil {
		t.Fatal("Quality modal opened despite encoder copy")
	}
}

func TestQualityPresetsUseWebmDefaultCodecWithNoEncoderStep(t *testing.T) {
	pl := pipeline.New(pipeline.Container{Format: pipeline.FormatWebM})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindQuality)

	out := viewText(m)
	if !strings.Contains(out, "Default (CRF 33)") {
		t.Errorf("Quality modal missing webm's default vp9 CRF, got:\n%s", out)
	}
}

func TestQualityPresetsUseCurrentEncoderDefaultCRF(t *testing.T) {
	pl := pipeline.New(pipeline.Encoder{Codec: pipeline.CodecVP9})
	m := New(testSession(pl))
	m = resized(m, 100, 30)
	m = openMenu(t, m, pipeline.KindQuality)

	out := viewText(m)
	if !strings.Contains(out, "Default (CRF 33)") {
		t.Errorf("Quality modal missing vp9's default CRF, got:\n%s", out)
	}
}
