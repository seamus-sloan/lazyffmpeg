package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/picker"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/probe"
)

func fakeEntries() []picker.Entry {
	return []picker.Entry{
		{Name: "sub", Path: "/dir/sub", IsDir: true},
		{Name: "clip-b.mp4", Path: "/dir/clip-b.mp4", Size: 512, ModTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{Name: "clip-a.mp4", Path: "/dir/clip-a.mp4", Size: 1024, ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
}

func pickerSession() app.Session {
	return app.Session{Dir: "/dir"}
}

func TestNewStartsInPickerModeWhenInputEmpty(t *testing.T) {
	var gotDir string
	m := New(pickerSession(), WithLister(func(dir string) ([]picker.Entry, error) {
		gotDir = dir
		return fakeEntries(), nil
	}))
	if m.mode != modePicker {
		t.Fatalf("mode = %v, want modePicker", m.mode)
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned no command in picker mode")
	}
	msg := cmd()
	if gotDir != "/dir" {
		t.Errorf("listFn called with dir %q, want /dir", gotDir)
	}
	listed, ok := msg.(pickerListedMsg)
	if !ok {
		t.Fatalf("Init() cmd produced %T, want pickerListedMsg", msg)
	}
	if len(listed.entries) != 3 {
		t.Errorf("listed %d entries, want 3", len(listed.entries))
	}
}

func TestPickerViewListsEntriesAndCount(t *testing.T) {
	m := New(pickerSession(), WithLister(func(dir string) ([]picker.Entry, error) {
		return fakeEntries(), nil
	}))
	m = resized(m, 100, 30)
	mm, cmd := m.Update(m.Init()())
	m = mm.(Model)
	_ = cmd

	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "lazyff · /dir") {
		t.Errorf("missing picker title, got:\n%s", out)
	}
	if !strings.Contains(out, "2 usable files") {
		t.Errorf("missing usable file count, got:\n%s", out)
	}
	for _, want := range []string{"sub/", "clip-b.mp4", "clip-a.mp4", ".."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing entry %q, got:\n%s", want, out)
		}
	}
}

func TestPickerEmptyListing(t *testing.T) {
	m := New(pickerSession(), WithLister(func(dir string) ([]picker.Entry, error) {
		return nil, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: nil})
	m = mm.(Model)
	m = resized(m, 100, 30)

	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "No video files in /dir") {
		t.Errorf("missing empty message, got:\n%s", out)
	}
	if !strings.Contains(out, "backspace") {
		t.Errorf("missing backspace hint, got:\n%s", out)
	}
}

func TestPickerNavigationKeys(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	if m.picker.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.picker.cursor)
	}

	mm, _ = m.Update(key("j"))
	m = mm.(Model)
	if m.picker.cursor != 1 {
		t.Fatalf("after j, cursor = %d, want 1", m.picker.cursor)
	}

	for i := 0; i < 10; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	if m.picker.cursor != 3 { // ".." + sub + clip-b + clip-a = 4 rows, max index 3
		t.Errorf("cursor did not clamp: got %d, want 3", m.picker.cursor)
	}

	mm, _ = m.Update(key("k"))
	m = mm.(Model)
	if m.picker.cursor != 2 {
		t.Errorf("after k, cursor = %d, want 2", m.picker.cursor)
	}
}

func TestPickerEnterOnDirectoryOpensIt(t *testing.T) {
	var listedDirs []string
	m := New(pickerSession(), WithLister(func(dir string) ([]picker.Entry, error) {
		listedDirs = append(listedDirs, dir)
		if dir == "/dir" {
			return fakeEntries(), nil
		}
		return nil, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	// Move onto "sub" (row 1: .. is row 0).
	mm, _ = m.Update(key("j"))
	m = mm.(Model)

	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("enter on a directory issued no command")
	}
	msg := cmd()
	listed, ok := msg.(pickerListedMsg)
	if !ok {
		t.Fatalf("enter on a directory produced %T, want pickerListedMsg", msg)
	}
	if listed.dir != "/dir/sub" {
		t.Errorf("listed dir = %q, want /dir/sub", listed.dir)
	}

	mm, _ = m.Update(listed)
	m = mm.(Model)
	if m.picker.dir != "/dir/sub" {
		t.Errorf("picker.dir = %q, want /dir/sub", m.picker.dir)
	}
	if m.picker.cursor != 0 {
		t.Errorf("cursor after opening a directory = %d, want 0", m.picker.cursor)
	}
}

func TestPickerBackspaceGoesToParent(t *testing.T) {
	m := New(app.Session{Dir: "/a/b"})
	mm, cmd := m.Update(key("backspace"))
	m = mm.(Model)
	if m.picker.dir != "/a" {
		t.Errorf("picker.dir = %q, want /a", m.picker.dir)
	}
	if cmd == nil {
		t.Fatal("backspace issued no listing command")
	}
}

func TestPickerBackspaceAtRootIsNoOp(t *testing.T) {
	m := New(app.Session{Dir: "/"})
	mm, cmd := m.Update(key("backspace"))
	m = mm.(Model)
	if m.picker.dir != "/" {
		t.Errorf("picker.dir = %q, want / (unchanged)", m.picker.dir)
	}
	if cmd != nil {
		t.Error("backspace at the filesystem root issued a command")
	}
}

func TestPickerFilterTypingAndEscClears(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	mm, _ = m.Update(key("/"))
	m = mm.(Model)
	if !m.picker.filtering {
		t.Fatal("/ did not start filtering")
	}

	for _, r := range "clip-a" {
		mm, _ = m.Update(key(string(r)))
		m = mm.(Model)
	}
	if m.picker.filter != "clip-a" {
		t.Fatalf("filter = %q, want %q", m.picker.filter, "clip-a")
	}

	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "clip-a.mp4") {
		t.Errorf("filtered view missing clip-a.mp4, got:\n%s", out)
	}
	if strings.Contains(out, "clip-b.mp4") {
		t.Errorf("filtered view should not contain clip-b.mp4, got:\n%s", out)
	}

	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.picker.filtering || m.picker.filter != "" {
		t.Errorf("esc did not clear the filter: filtering=%v filter=%q", m.picker.filtering, m.picker.filter)
	}
}

func TestPickerFilterEnterAccepts(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)

	mm, _ = m.Update(key("/"))
	m = mm.(Model)
	mm, _ = m.Update(key("a"))
	m = mm.(Model)
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)

	if m.picker.filtering {
		t.Error("enter did not stop filtering")
	}
	if m.picker.filter != "a" {
		t.Errorf("filter after enter = %q, want %q (kept)", m.picker.filter, "a")
	}
}

func TestPickerEnterOnFileProbesAsynchronously(t *testing.T) {
	wantInfo := probe.Info{Duration: 5, Video: probe.VideoStream{Width: 10, Height: 10, Codec: "h264"}}
	m := New(pickerSession(), WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		if path != "/dir/clip-a.mp4" {
			t.Errorf("probed %q, want /dir/clip-a.mp4", path)
		}
		return wantInfo, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	// Row order: .. , sub, clip-b.mp4, clip-a.mp4 -> clip-a.mp4 is row 3.
	for i := 0; i < 3; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}

	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("enter on a file issued no command")
	}
	msg := cmd()
	probed, ok := msg.(pickerProbedMsg)
	if !ok {
		t.Fatalf("enter on a file produced %T, want pickerProbedMsg", msg)
	}

	mm, _ = m.Update(probed)
	m = mm.(Model)
	if m.mode != modeMain {
		t.Fatalf("mode after a successful probe = %v, want modeMain", m.mode)
	}
	if m.session.Input != "/dir/clip-a.mp4" {
		t.Errorf("session.Input = %q, want /dir/clip-a.mp4", m.session.Input)
	}
	if m.session.Info.Duration != 5 {
		t.Errorf("session.Info.Duration = %v, want 5", m.session.Info.Duration)
	}
}

func TestPickerProbeFailureStaysOnPicker(t *testing.T) {
	wantErr := errors.New("ffprobe: boom")
	m := New(pickerSession(), WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		return probe.Info{}, wantErr
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	mm, _ = m.Update(pickerProbedMsg{path: "/dir/clip-a.mp4", err: wantErr})
	m = mm.(Model)
	if m.mode != modePicker {
		t.Fatalf("mode after a failed probe = %v, want modePicker", m.mode)
	}
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "ffprobe: boom") {
		t.Errorf("missing probe error on picker status line, got:\n%s", out)
	}
}

func TestPickerSeededPipelineCarriesOverToMainSession(t *testing.T) {
	seeded := pipeline.New(pipeline.Speed{Factor: 2})
	s := app.Session{Dir: "/dir", Pipeline: seeded, Output: "/out.mp4", Force: true}
	m := New(s, WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		return probe.Info{Duration: 5}, nil
	}))
	mm, _ := m.Update(pickerProbedMsg{path: "/dir/clip-a.mp4", info: probe.Info{Duration: 5}})
	m = mm.(Model)

	if _, ok := m.pipeline.Find(pipeline.KindSpeed); !ok {
		t.Error("seeded Speed step lost across the picker -> main transition")
	}
	if m.session.Output != "/out.mp4" || !m.session.Force {
		t.Errorf("session flags lost: Output=%q Force=%v", m.session.Output, m.session.Force)
	}
}

func TestPickerHelpOverlay(t *testing.T) {
	m := New(pickerSession())
	m = resized(m, 100, 30)

	mm, _ := m.Update(key("?"))
	m = mm.(Model)
	if !m.showHelp {
		t.Fatal("? did not open help in picker mode")
	}
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "parent directory") {
		t.Errorf("picker help missing key hints, got:\n%s", out)
	}
}

func TestPickerQuit(t *testing.T) {
	m := New(pickerSession())
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg")
	}
}
