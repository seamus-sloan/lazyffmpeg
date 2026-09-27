package tui

import (
	"context"
	"errors"
	"fmt"
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

func TestPickerInitialCursorSkipsParentRow(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	if m.picker.cursor != 1 {
		t.Errorf("initial cursor = %d, want 1 (the first real entry, past \"..\")", m.picker.cursor)
	}
}

func TestPickerInitialCursorOnParentRowWhenItIsTheOnlyRow(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: nil})
	m = mm.(Model)
	if m.picker.cursor != 0 {
		t.Errorf("cursor with no entries = %d, want 0 (\"..\" is the only row)", m.picker.cursor)
	}
}

func TestPickerNavigationKeys(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	if m.picker.cursor != 1 {
		t.Fatalf("initial cursor = %d, want 1", m.picker.cursor)
	}

	mm, _ = m.Update(key("j"))
	m = mm.(Model)
	if m.picker.cursor != 2 {
		t.Fatalf("after j, cursor = %d, want 2", m.picker.cursor)
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

func TestPickerViewFitsHeightWithManyEntries(t *testing.T) {
	var es []picker.Entry
	for i := 0; i < 60; i++ {
		es = append(es, picker.Entry{Name: fmt.Sprintf("clip-%02d.mp4", i), Path: fmt.Sprintf("/dir/clip-%02d.mp4", i)})
	}
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: es})
	m = mm.(Model)
	m = resized(m, 100, 30)

	out := viewText(m)
	if h := strings.Count(out, "\n") + 1; h > 30 {
		t.Fatalf("picker view is %d lines tall in a 30-line terminal, want <= 30:\n%s", h, out)
	}

	// Moving the cursor down past the visible window must scroll the
	// list so the last entry stays visible.
	for i := 0; i < 65; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	out = viewText(m)
	if !strings.Contains(out, "clip-59.mp4") {
		t.Errorf("last entry not visible after scrolling to it, got:\n%s", out)
	}
	if h := strings.Count(out, "\n") + 1; h > 30 {
		t.Errorf("picker view is %d lines tall after scrolling, want <= 30:\n%s", h, out)
	}
}

// runeIndex is strings.Index measured in runes (display columns) rather
// than bytes, so a multi-byte character (e.g. the truncation ellipsis)
// earlier in the line does not throw off the reported position.
func runeIndex(s, substr string) int {
	byteIdx := strings.Index(s, substr)
	if byteIdx < 0 {
		return -1
	}
	return len([]rune(s[:byteIdx]))
}

func TestPickerColumnsAlignRegardlessOfNameLength(t *testing.T) {
	entries := []picker.Entry{
		{Name: "short.mp4", Path: "/dir/short.mp4", Size: 100, ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Name: strings.Repeat("x", 80) + ".mp4", Path: "/dir/long.mp4", Size: 200, ModTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: entries})
	m = mm.(Model)
	m = resized(m, 100, 30)

	out := viewText(m)
	col1, col2 := -1, -1
	for _, l := range strings.Split(out, "\n") {
		if i := runeIndex(l, "100 B"); i >= 0 {
			col1 = i
		}
		if i := runeIndex(l, "200 B"); i >= 0 {
			col2 = i
		}
	}
	if col1 == -1 || col2 == -1 {
		t.Fatalf("size column not found in view, got:\n%s", out)
	}
	if col1 != col2 {
		t.Errorf("size columns misaligned: %d vs %d, got:\n%s", col1, col2, out)
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

	// The cursor already starts on "sub" (row 1: .. is row 0, skipped).
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

// filteringPicker is a picker listing fakeEntries in /dir, with its
// filter open and text typed into it.
func filteringPicker(t *testing.T, text string) Model {
	t.Helper()
	m := New(pickerSession(), WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		return probe.Info{}, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = resized(mm.(Model), 100, 30)
	for _, k := range append([]string{"/"}, strings.Split(text, "")...) {
		mm, _ = m.Update(key(k))
		m = mm.(Model)
	}
	return m
}

func TestPickerFilterThenEnterTwiceOpensTheMatch(t *testing.T) {
	m := filteringPicker(t, "clip-a")
	for _, k := range []string{"enter", "enter"} {
		mm, _ := m.Update(key(k))
		m = mm.(Model)
	}
	if m.picker.dir != "/dir" || m.picker.probingPath != "/dir/clip-a.mp4" {
		t.Fatalf("filter clip-a, enter, enter: dir=%q probingPath=%q, want /dir/clip-a.mp4 probed", m.picker.dir, m.picker.probingPath)
	}
}

func TestPickerFilterEditsPutTheCursorOnTheFirstMatch(t *testing.T) {
	m := filteringPicker(t, "clip-a")
	if m.picker.cursor != 1 {
		t.Errorf("cursor after typing a filter = %d, want 1 (the first match, past \"..\")", m.picker.cursor)
	}

	mm, _ := m.Update(key("backspace"))
	m = mm.(Model)
	if m.picker.cursor != 1 {
		t.Errorf("cursor after backspace = %d, want 1", m.picker.cursor)
	}

	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.picker.cursor != 1 {
		t.Errorf("cursor after esc clears the filter = %d, want 1", m.picker.cursor)
	}
}

func TestPickerFilterWithNoMatchesPutsTheCursorOnParent(t *testing.T) {
	m := filteringPicker(t, "no-such-clip")
	if m.picker.cursor != 0 {
		t.Errorf("cursor with nothing matching = %d, want 0 (\"..\" is the only row)", m.picker.cursor)
	}
}

func TestPickerAcceptingTheFilterKeepsTheCursor(t *testing.T) {
	m := filteringPicker(t, "clip")
	mm, _ := m.Update(key("down"))
	m = mm.(Model)
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)
	if m.picker.cursor != 2 {
		t.Errorf("cursor after moving down and accepting the filter = %d, want 2", m.picker.cursor)
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

func TestPickerMultiLineProbeErrorShowsItsLastLineOnOneRow(t *testing.T) {
	m := New(pickerSession())
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = resized(mm.(Model), 100, 30)
	mm, _ = m.Update(pickerProbedMsg{err: errors.New("ffprobe: exit status 1: noise\nmore noise\n  clip.mp4: Invalid data found  \n\n")})
	m = mm.(Model)

	out := viewText(m)
	if !strings.Contains(out, "│clip.mp4: Invalid data found ") {
		t.Errorf("picker status is not the error's last line, trimmed, on one row:\n%s", out)
	}
	if strings.Contains(out, "noise") {
		t.Errorf("picker status kept the error's earlier lines:\n%s", out)
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

	// Row order: .. , sub, clip-b.mp4, clip-a.mp4 -> clip-a.mp4 is row 3.
	for i := 0; i < 3; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	msg := cmd()

	mm, _ = m.Update(msg)
	m = mm.(Model)
	if m.mode != modePicker {
		t.Fatalf("mode after a failed probe = %v, want modePicker", m.mode)
	}
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "ffprobe: boom") {
		t.Errorf("missing probe error on picker status line, got:\n%s", out)
	}
}

func TestPickerIgnoresEnterWhileProbing(t *testing.T) {
	m := New(pickerSession(), WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		return probe.Info{Duration: 5}, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	// Start probing clip-a.mp4 (row 3).
	for i := 0; i < 3; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("enter on a file issued no command")
	}
	if !m.picker.probing {
		t.Fatal("picker.probing was not set")
	}

	// A second enter while the first probe is still pending must not
	// start a second probe.
	mm, cmd2 := m.Update(key("enter"))
	m = mm.(Model)
	if cmd2 != nil {
		t.Error("enter while probing issued another probe command")
	}

	// The pending probe still completes normally.
	mm, _ = m.Update(cmd())
	m = mm.(Model)
	if m.mode != modeMain {
		t.Fatalf("mode after the pending probe completed = %v, want modeMain", m.mode)
	}
}

func TestPickerIgnoresStaleProbeForADifferentPath(t *testing.T) {
	m := New(pickerSession(), WithProber(func(ctx context.Context, path string) (probe.Info, error) {
		return probe.Info{Duration: 5}, nil
	}))
	mm, _ := m.Update(pickerListedMsg{dir: "/dir", entries: fakeEntries()})
	m = mm.(Model)
	m = resized(m, 100, 30)

	// Start probing clip-a.mp4 (row 3), then feed a stale result for a
	// different path: it must be ignored, leaving the pending probe (and
	// picker mode) untouched.
	for i := 0; i < 3; i++ {
		mm, _ = m.Update(key("j"))
		m = mm.(Model)
	}
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)

	mm, _ = m.Update(pickerProbedMsg{path: "/dir/clip-b.mp4", info: probe.Info{Duration: 9}})
	m = mm.(Model)
	if m.mode != modePicker {
		t.Fatalf("mode after a stale probe = %v, want modePicker", m.mode)
	}
	if !m.picker.probing {
		t.Error("a stale probe result cleared the pending probe")
	}
}

func TestPickerLateProbeAfterSwitchingToMainDoesNotOverwriteSession(t *testing.T) {
	m := New(pickerSession())
	m = resized(m, 100, 30)

	mm, _ := m.Update(pickerProbedMsg{path: "/dir/clip-b.mp4", info: probe.Info{Duration: 5}})
	m = mm.(Model)
	m.undo = []pipeline.Pipeline{pipeline.New()}

	mm, _ = m.Update(pickerProbedMsg{path: "/dir/clip-a.mp4", info: probe.Info{Duration: 9}})
	m = mm.(Model)
	if m.session.Input != "/dir/clip-b.mp4" || len(m.undo) != 1 {
		t.Fatalf("a late probe for a different file switched the editor to %s, undo depth %d",
			m.session.Input, len(m.undo))
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
