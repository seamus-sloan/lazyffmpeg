package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/runner"
)

func TestMenuListsFileNameBetweenContainerAndRawArgs(t *testing.T) {
	out := viewText(resized(New(testSession(pipeline.New())), 100, 30))

	container := strings.Index(out, "  Container")
	fileName := strings.Index(out, "  File name")
	rawArgs := strings.Index(out, "  Raw args")
	if fileName < 0 {
		t.Fatalf("MENU does not list File name:\n%s", out)
	}
	if !(container < fileName && fileName < rawArgs) {
		t.Errorf("MENU order: Container at %d, File name at %d, Raw args at %d; want File name between them:\n%s",
			container, fileName, rawArgs, out)
	}
}

func TestFileNameModalPrefillsTheCurrentOutputName(t *testing.T) {
	withOutput := testSession(pipeline.New())
	withOutput.Output = "/out/x.mkv"
	cases := []struct {
		name string
		m    Model
		want string
	}{
		{"default name", New(testSession(pipeline.New())), "clip (edited).mov"},
		{"container step's extension", New(testSession(pipeline.New(pipeline.Container{Format: pipeline.FormatMKV}))), "clip (edited).mkv"},
		{"-o's name", New(withOutput), "x.mkv"},
		{"existing File name step, as given", New(testSession(pipeline.New(pipeline.Filename{Name: "demo"}))), "demo"},
	}
	for _, c := range cases {
		m := openMenu(t, resized(c.m, 100, 30), pipeline.KindFilename)
		if m.modal == nil || m.modal.kind != pipeline.KindFilename {
			t.Fatalf("%s: File name modal did not open: %v", c.name, m.modal)
		}
		if !isCustomOption(m.modal.options, m.modal.cursor) || !m.modal.input.Focused() {
			t.Errorf("%s: the text input is not selected and focused", c.name)
		}
		if got := m.modal.input.Value(); got != c.want {
			t.Errorf("%s: prefill = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFileNameModalHasNoPresetsAndShowsTheExtensionHint(t *testing.T) {
	m := openMenu(t, resized(New(testSession(pipeline.New())), 100, 30), pipeline.KindFilename)
	if len(m.modal.options) != 1 {
		t.Errorf("File name modal has %d options, want just the text input", len(m.modal.options))
	}
	out := viewText(m)
	if !strings.Contains(out, "name (.mp4/.mov/.mkv/.webm/.m4v sets the container)") {
		t.Errorf("File name modal does not show the extension hint:\n%s", out)
	}
}

// renameTo opens File name, clears its prefilled input, types name and
// confirms it.
func renameTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = openMenu(t, m, pipeline.KindFilename)
	for range []rune(m.modal.input.Value()) {
		mm, _ := m.Update(key("backspace"))
		m = mm.(Model)
	}
	m = typeText(m, name)
	mm, _ := m.Update(key("enter"))
	return mm.(Model)
}

func TestFileNameConfirmAddsTheStepAndChangesTheFooterPath(t *testing.T) {
	m := resized(New(testSession(pipeline.New())), 100, 30)
	if out := viewText(m); !strings.Contains(out, "→ clip (edited).mov") {
		t.Fatalf("footer does not start on the default output:\n%s", out)
	}

	m = renameTo(t, m, "demo.mp4")

	if m.modal != nil {
		t.Fatalf("modal still open, error %q", m.modal.err)
	}
	if s, ok := m.pipeline.Find(pipeline.KindFilename); !ok || s.(pipeline.Filename).Name != "demo.mp4" {
		t.Fatalf("pipeline File name = %v, want demo.mp4", s)
	}
	out := viewText(m)
	if !strings.Contains(out, "1. File name    demo.mp4") {
		t.Errorf("PIPELINE does not show the File name step:\n%s", out)
	}
	if !strings.Contains(out, "→ demo.mp4") || strings.Contains(out, "(edited)") {
		t.Errorf("footer output path is not the new name:\n%s", out)
	}
}

func TestRemovingFileNameRestoresTheDefaultNameAndUndoBringsItBack(t *testing.T) {
	m := renameTo(t, resized(New(testSession(pipeline.New())), 100, 30), "demo.mp4")
	mm, _ := m.Update(key("tab"))
	m = mm.(Model)

	mm, _ = m.Update(key("x"))
	m = mm.(Model)
	if out := viewText(m); !strings.Contains(out, "→ clip (edited).mov") {
		t.Errorf("x did not put the default output name back:\n%s", out)
	}

	mm, _ = m.Update(key("u"))
	m = mm.(Model)
	if out := viewText(m); !strings.Contains(out, "→ demo.mp4") {
		t.Errorf("u did not bring the File name step back:\n%s", out)
	}
}

func TestFileNameModalRejectsAPath(t *testing.T) {
	m := renameTo(t, resized(New(testSession(pipeline.New())), 100, 30), "clips/demo.mp4")
	if m.modal == nil {
		t.Fatal("modal closed on a name holding a /")
	}
	if !strings.Contains(viewText(m), "must not contain /") {
		t.Errorf("modal does not explain the rejected name:\n%s", viewText(m))
	}
	if _, ok := m.pipeline.Find(pipeline.KindFilename); ok {
		t.Error("a rejected name was added to the pipeline")
	}
}

func TestInPlaceSessionRefusesToRunWithAFileName(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mov")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	}
	s := app.Session{Input: in, Info: testInfo(), InPlace: true, Force: true}
	m := renameTo(t, resized(New(s, WithRunner(fake)), 100, 30), "demo.mov")
	if _, ok := m.pipeline.Find(pipeline.KindFilename); !ok {
		t.Fatal("File name step was not added")
	}

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("r issued a run command despite the File name step in an in-place session")
	}
	if calls != 0 {
		t.Errorf("RunFunc called %d times, want 0", calls)
	}
	if m.run.phase != runError {
		t.Fatalf("run.phase = %v, want runError", m.run.phase)
	}
	if out := viewText(m); !strings.Contains(out, app.ErrInPlaceRename.Error()) {
		t.Errorf("view does not explain the refusal, got:\n%s", out)
	}
}

func TestFileNameOfAnExistingFileAsksBeforeOverwriting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "demo.mov")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		return runner.Result{}, nil
	}
	s := app.Session{Input: filepath.Join(dir, "clip.mov"), Info: testInfo()}
	m := renameTo(t, resized(New(s, WithRunner(fake)), 200, 30), "demo")

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil || m.run.phase != runConfirmOverwrite {
		t.Fatalf("run.phase = %v (cmd %v), want runConfirmOverwrite with no run started", m.run.phase, cmd != nil)
	}
	if want := "Overwrite " + existing + "? (y/n)"; !strings.Contains(viewText(m), want) {
		t.Errorf("missing overwrite prompt %q, got:\n%s", want, viewText(m))
	}
}

func TestFileNameOfTheInputIsRefusedAsTheInputItself(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "clip.mov")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int
	fake := func(ctx context.Context, job runner.Job, onProgress func(runner.Progress)) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	}
	s := app.Session{Input: in, Info: testInfo(), Force: true}
	m := renameTo(t, resized(New(s, WithRunner(fake)), 100, 30), "clip")

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd != nil || calls != 0 {
		t.Fatalf("a run started (cmd %v, calls %d) for a name that is the input itself", cmd != nil, calls)
	}
	out := viewText(m)
	if !strings.Contains(out, `file name "clip" is the input file; choose a different name`) {
		t.Errorf("view does not say the file name is the input, got:\n%s", out)
	}
	if strings.Contains(out, "--in-place") {
		t.Errorf("view suggests --in-place, which cannot rename, got:\n%s", out)
	}
}

func TestFileNameModalRejectsAHiddenFileName(t *testing.T) {
	m := renameTo(t, resized(New(testSession(pipeline.New())), 100, 30), ".hidden")
	if m.modal == nil {
		t.Fatal("modal closed on a name starting with .")
	}
	if !strings.Contains(viewText(m), "must not start with") {
		t.Errorf("modal does not explain the rejected name:\n%s", viewText(m))
	}
	if _, ok := m.pipeline.Find(pipeline.KindFilename); ok {
		t.Error("a rejected name was added to the pipeline")
	}
}

func TestFooterShowsAFileNameTooLongOnceItsExtensionIsAppended(t *testing.T) {
	m := resized(New(testSession(pipeline.New(pipeline.Filename{Name: strings.Repeat("n", 252)}))), 100, 30)
	if out := viewText(m); !strings.Contains(out, "file name too long: 256 bytes once .mov is appended") {
		t.Errorf("footer does not show the file-name-too-long error:\n%s", out)
	}
}
