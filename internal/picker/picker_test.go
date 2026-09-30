package picker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsable(t *testing.T) {
	cases := map[string]bool{
		"clip.mp4":    true,
		"clip.MP4":    true,
		"clip.m4v":    true,
		"clip.mov":    true,
		"clip.qt":     true,
		"clip.mkv":    true,
		"clip.webm":   true,
		"clip.avi":    true,
		"clip.wmv":    true,
		"clip.asf":    true,
		"clip.flv":    true,
		"clip.f4v":    true,
		"clip.mpg":    true,
		"clip.mpeg":   true,
		"clip.m2v":    true,
		"clip.vob":    true,
		"clip.ts":     true,
		"clip.mts":    true,
		"clip.m2ts":   true,
		"clip.3gp":    true,
		"clip.3g2":    true,
		"clip.ogv":    true,
		"clip.mxf":    true,
		"clip.dv":     true,
		"clip.y4m":    true,
		"clip.gif":    true,
		"song.mp3":    false,
		"song.wav":    false,
		"photo.png":   true,
		"photo.JPG":   true,
		"photo.jpeg":  true,
		"photo.avif":  true,
		"photo.webp":  true,
		"photo.heic":  true,
		"photo.tiff":  true,
		"photo.psd":   false,
		"README":      false,
		"README.md":   false,
		"noextension": false,
	}
	for name, want := range cases {
		if got := Usable(name); got != want {
			t.Errorf("Usable(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestListOrdersDirsThenFilesByModTime(t *testing.T) {
	dir := t.TempDir()

	mustDir := func(name string) {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustFile := func(name string, mod time.Time) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}

	mustDir("Zebra")
	mustDir("apple")
	mustFile("old.mp4", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	mustFile("new.mp4", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))
	mustFile("tie-b.mp4", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	mustFile("tie-a.mp4", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	mustFile("clip PM.mov", time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC))
	mustFile("not-a-video.txt", time.Now())
	mustFile(".hidden.mp4", time.Now())

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{"apple", "Zebra", "new.mp4", "clip PM.mov", "tie-a.mp4", "tie-b.mp4", "old.mp4"}
	if len(names) != len(want) {
		t.Fatalf("List() names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("List() names = %v, want %v", names, want)
			break
		}
	}

	for _, e := range entries[:2] {
		if !e.IsDir {
			t.Errorf("entry %q should be a directory", e.Name)
		}
	}
	for _, e := range entries[2:] {
		if e.IsDir {
			t.Errorf("entry %q should not be a directory", e.Name)
		}
	}
}

func TestListSkipsBrokenSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing.mp4"), filepath.Join(dir, "broken.mp4")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List() = %v, want empty (broken symlink skipped)", entries)
	}
}

func TestListUnreadableDirErrors(t *testing.T) {
	if _, err := List(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("List on a missing directory: want error, got nil")
	}
}
