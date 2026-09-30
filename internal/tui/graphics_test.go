package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
)

// cmdMsgs runs cmd and returns the messages it produces, flattening
// batches.
func cmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, cmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// settle feeds msg through Update, then every message its commands
// produce (and theirs, in turn), and returns the resulting model with
// everything those commands wrote straight to the terminal, in order.
func settle(t *testing.T, m Model, msg tea.Msg) (Model, string) {
	t.Helper()
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	var raw strings.Builder
	for _, next := range cmdMsgs(cmd) {
		if r, ok := next.(tea.RawMsg); ok {
			raw.WriteString(fmt.Sprint(r.Msg))
			continue
		}
		var more string
		m, more = settle(t, m, next)
		raw.WriteString(more)
	}
	return m, raw.String()
}

// imageRenderer returns a RenderFunc that records every request and
// answers each with "IMG<id>" in place of a kitty image sequence.
func imageRenderer() (RenderFunc, *[]preview.Request) {
	reqs := &[]preview.Request{}
	fn := func(ctx context.Context, req preview.Request) (string, error) {
		*reqs = append(*reqs, req)
		return fmt.Sprintf("IMG%d", req.ImageID), nil
	}
	return fn, reqs
}

// drawnAtOrigin is the output that draws image id's placeholder text at
// the preview box's origin.
func drawnAtOrigin(id int) string {
	x, y := previewOrigin()
	return ansi.SaveCursor + ansi.CursorPosition(x+1, y+1) + fmt.Sprintf("IMG%d", id) + ansi.RestoreCursor
}

var probeOK = uv.KittyGraphicsEvent{Options: kitty.Options{ID: graphicsProbeID}, Payload: []byte("OK")}

// graphicsModel is a 100x30 main screen whose terminal reported a
// 1000x600 pixel window (10x20 px cells) and answered the graphics probe,
// with chafa absent, its first image drawn.
func graphicsModel(t *testing.T) (Model, *[]preview.Request) {
	t.Helper()
	fn, reqs := imageRenderer()
	m := New(testSession(pipeline.New()), WithRenderer(fn, false))
	m, _ = settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = settle(t, m, uv.PixelSizeEvent{Width: 1000, Height: 600})
	m, _ = settle(t, m, probeOK)
	return m, reqs
}

func TestInitSendsGraphicsProbe(t *testing.T) {
	m := New(testSession(pipeline.New()))
	var raw []string
	for _, msg := range cmdMsgs(m.Init()) {
		if r, ok := msg.(tea.RawMsg); ok {
			raw = append(raw, fmt.Sprint(r.Msg))
		}
	}
	if len(raw) != 1 || raw[0] != graphicsProbe {
		t.Fatalf("Init wrote %q, want the graphics probe", raw)
	}
	if !strings.Contains(graphicsProbe, "a=q") || !strings.HasSuffix(graphicsProbe, "\x1b[14t") {
		t.Errorf("graphicsProbe = %q, want a kitty query then a window pixel size request", graphicsProbe)
	}
}

func TestProbeReplySwitchesPreviewToImages(t *testing.T) {
	fn, reqs := imageRenderer()
	m := New(testSession(pipeline.New()), WithRenderer(fn, false))
	m, _ = settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = settle(t, m, uv.PixelSizeEvent{Width: 1000, Height: 600})
	if len(*reqs) != 0 {
		t.Fatalf("rendered %d frames without chafa or graphics, want 0", len(*reqs))
	}
	if !strings.Contains(viewText(m), "install chafa for preview") {
		t.Errorf("preview box does not ask for chafa, got:\n%s", viewText(m))
	}

	m, raw := settle(t, m, probeOK)

	if len(*reqs) != 1 {
		t.Fatalf("renderer called %d times after the probe reply, want 1", len(*reqs))
	}
	req := (*reqs)[0]
	cols, rows := m.previewBoxSize()
	if req.ImageID != imageIDs[0] || req.PixelWidth != cols*10 || req.PixelHeight != rows*20 {
		t.Errorf("request = %+v, want ImageID %d, %dx%d px (10x20 px cells)", req, imageIDs[0], cols*10, rows*20)
	}
	if raw != drawnAtOrigin(imageIDs[0]) {
		t.Errorf("wrote %q, want the image drawn at the box's origin", raw)
	}
	if out := viewText(m); strings.Contains(out, "IMG") || strings.Contains(out, "install chafa") {
		t.Errorf("preview box should be left blank under the image, got:\n%s", out)
	}
}

func TestProbeReplyForAnotherImageIsIgnored(t *testing.T) {
	fn, reqs := imageRenderer()
	m := New(testSession(pipeline.New()), WithRenderer(fn, false))
	m, _ = settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = settle(t, m, uv.KittyGraphicsEvent{Options: kitty.Options{ID: 7}, Payload: []byte("OK")})
	if m.graphics || len(*reqs) != 0 {
		t.Errorf("graphics = %v after %d renders; a reply to another image must not enable it", m.graphics, len(*reqs))
	}
}

func TestNextImageIsDrawnBeforeThePreviousIsDeleted(t *testing.T) {
	m, reqs := graphicsModel(t)

	m, raw := settle(t, m, key("l"))

	if got := (*reqs)[len(*reqs)-1].ImageID; got != imageIDs[1] {
		t.Fatalf("next frame's ImageID = %d, want the other id %d", got, imageIDs[1])
	}
	if want := drawnAtOrigin(imageIDs[1]) + preview.DeleteKittyImage(imageIDs[0]); raw != want {
		t.Errorf("wrote %q, want %q", raw, want)
	}

	_, raw = settle(t, m, key("l"))
	if want := drawnAtOrigin(imageIDs[0]) + preview.DeleteKittyImage(imageIDs[1]); raw != want {
		t.Errorf("third frame wrote %q, want %q", raw, want)
	}
}

func TestImageHiddenUnderHelpAndRedrawnAfter(t *testing.T) {
	m, _ := graphicsModel(t)

	m, raw := settle(t, m, key("?"))
	if want := preview.DeleteKittyImage(imageIDs[0]); raw != want {
		t.Errorf("opening help wrote %q, want %q", raw, want)
	}

	_, raw = settle(t, m, key("x"))
	if want := drawnAtOrigin(imageIDs[0]); raw != want {
		t.Errorf("closing help wrote %q, want %q", raw, want)
	}
}

func TestImageHiddenUntilRerenderedForNewBoxSize(t *testing.T) {
	m, reqs := graphicsModel(t)

	// The resize itself deletes the stale image, before its rerender lands.
	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	var raw string
	var frame tea.Msg
	for _, msg := range cmdMsgs(cmd) {
		switch msg := msg.(type) {
		case tea.RawMsg:
			raw += fmt.Sprint(msg.Msg)
		case previewFrameMsg:
			frame = msg
		}
	}
	if want := ansi.WindowOp(14) + preview.DeleteKittyImage(imageIDs[0]); raw != want {
		t.Errorf("resize wrote %q, want the pixel size queried again and the stale image deleted (%q)", raw, want)
	}
	if frame == nil {
		t.Fatal("resize issued no rerender")
	}
	cols, rows := m.previewBoxSize()
	if last := (*reqs)[len(*reqs)-1]; last.Cols != cols || last.Rows != rows {
		t.Errorf("rerendered for %dx%d, want the new box %dx%d", last.Cols, last.Rows, cols, rows)
	}

	_, raw = settle(t, m, frame)
	if want := drawnAtOrigin(imageIDs[1]); raw != want {
		t.Errorf("rerender wrote %q, want %q", raw, want)
	}
}

func TestPreviewOriginIsTheBoxContentCell(t *testing.T) {
	m := resized(New(testSession(pipeline.New())), 100, 30)
	lines := strings.Split(viewText(m), "\n")
	x, y := previewOrigin()
	cell := func(row, col int) string { return string([]rune(lines[row])[col]) }
	if got := cell(y-1, x-1); got != "┌" {
		t.Errorf("cell above-left of the origin = %q, want the box's corner ┌", got)
	}
	if got := cell(y, x-1); got != "│" {
		t.Errorf("cell left of the origin = %q, want the box's side │", got)
	}
}
