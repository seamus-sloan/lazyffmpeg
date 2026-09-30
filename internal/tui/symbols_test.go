package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/preview"
)

// fakeClipboard returns a ClipboardFunc that records what it is given
// and fails with err (when non-nil).
func fakeClipboard(err error) (ClipboardFunc, *[]string) {
	got := &[]string{}
	return func(text string) error {
		*got = append(*got, text)
		return err
	}, got
}

// copied presses y on m and settles the copy, returning the model and the
// plain request it rendered.
func copied(t *testing.T, m Model, reqs *[]preview.Request) (Model, preview.Request) {
	t.Helper()
	before := len(*reqs)
	m = step(t, m, key("y"))
	if len(*reqs) != before+1 || !(*reqs)[before].Plain {
		t.Fatalf("y issued %d renders (%+v), want one plain render", len(*reqs)-before, (*reqs)[before:])
	}
	return m, (*reqs)[before]
}

func TestYCopiesThePreviewFrameAsSymbols(t *testing.T) {
	fn, reqs := fakeRenderer("⣿⣿\n⠀⠀", nil)
	clip, got := fakeClipboard(nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true), WithClipboard(clip))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("L"))

	m, req := copied(t, m, reqs)

	if req.Cols != 100 || req.Time != 5 || req.Filter != "" {
		t.Errorf("request = %+v, want 100 columns of the original frame at 5s", req)
	}
	if len(*got) != 1 || (*got)[0] != "⣿⣿\n⠀⠀" {
		t.Errorf("clipboard got %q, want the rendered symbols", *got)
	}
	if want := "copied the frame at 00:05 as 100×2 symbols"; !strings.Contains(viewText(m), want) {
		t.Errorf("footer does not say %q, got:\n%s", want, viewText(m))
	}
}

func TestYCopiesTheResultFrameInResultMode(t *testing.T) {
	pl := pipeline.New(pipeline.Resolution{Width: 640, Height: 360, Exact: true})
	fn, reqs := fakeRenderer("⣿", nil)
	clip, _ := fakeClipboard(nil)
	m := New(testSession(pl), WithRenderer(fn, true), WithClipboard(clip))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, key("v"))

	_, req := copied(t, m, reqs)

	if want := pipeline.SpatialVideoFilter(pl); req.Filter != want {
		t.Errorf("Filter = %q, want the pipeline's spatial filter %q", req.Filter, want)
	}
}

func TestYFallsBackToTheTerminalClipboard(t *testing.T) {
	fn, _ := fakeRenderer("⣿⣿", nil)
	clip, _ := fakeClipboard(errors.New("no pbcopy"))
	m := New(testSession(pipeline.New()), WithRenderer(fn, true), WithClipboard(clip))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	mm, cmd := m.Update(key("y"))
	m = mm.(Model)
	mm, cmd = m.Update(cmd())
	m = mm.(Model)

	var sent []string
	for _, msg := range cmdMsgs(cmd) {
		sent = append(sent, fmt.Sprint(msg))
	}
	if len(sent) != 1 || sent[0] != "⣿⣿" {
		t.Errorf("sent %q, want the symbols handed to the terminal's clipboard", sent)
	}
	if !strings.Contains(viewText(m), "(via the terminal)") {
		t.Errorf("footer does not say the terminal took it, got:\n%s", viewText(m))
	}
}

func TestYWithoutChafaSaysSo(t *testing.T) {
	fn, reqs := fakeRenderer("⣿", nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, false))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = step(t, m, key("y"))

	if len(*reqs) != 0 {
		t.Errorf("rendered %d frames without chafa, want 0", len(*reqs))
	}
	if !strings.Contains(viewText(m), "install chafa to copy the frame as symbols") {
		t.Errorf("footer does not ask for chafa, got:\n%s", viewText(m))
	}
}

func TestYReportsARenderFailure(t *testing.T) {
	fn, _ := fakeRenderer("", errors.New("ffmpeg: exit status 1: boom"))
	clip, got := fakeClipboard(nil)
	m := New(testSession(pipeline.New()), WithRenderer(fn, true), WithClipboard(clip))
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = step(t, m, key("y"))

	if len(*got) != 0 {
		t.Errorf("clipboard got %q after a failed render, want nothing", *got)
	}
	if !strings.Contains(viewText(m), "copying the frame failed: ffmpeg: exit status 1: boom") {
		t.Errorf("footer does not report the failure, got:\n%s", viewText(m))
	}
}
