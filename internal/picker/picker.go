// Package picker lists a directory's usable video files and
// sub-directories for lazyff's file picker: no other project's file
// association tables are consulted, just a fixed extension allow-list.
package picker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one row in a directory listing: a sub-directory or a usable
// video file.
type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// usableExts are the file extensions (lowercase, with the leading dot)
// lazyff can open.
var usableExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".qt": true, ".mkv": true,
	".webm": true, ".avi": true, ".wmv": true, ".asf": true, ".flv": true,
	".f4v": true, ".mpg": true, ".mpeg": true, ".m2v": true, ".vob": true,
	".ts": true, ".mts": true, ".m2ts": true, ".3gp": true, ".3g2": true,
	".ogv": true, ".mxf": true, ".dv": true, ".y4m": true, ".gif": true,
}

// Usable reports whether name's extension is one lazyff can open,
// case-insensitively. Names without a recognized extension (including no
// extension at all) are not usable.
func Usable(name string) bool {
	return usableExts[strings.ToLower(filepath.Ext(name))]
}

// List returns dir's entries: sub-directories first (alphabetical,
// case-insensitive), then usable files newest-modified first (ties broken
// by name). Hidden entries (a leading '.') are skipped. Symlinks are
// followed to decide file vs directory; broken symlinks are skipped.
func List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var dirs, files []Entry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		info, statErr := os.Stat(full) // follows symlinks
		if statErr != nil {
			continue // broken symlink or otherwise unreadable
		}
		if info.IsDir() {
			dirs = append(dirs, Entry{Name: name, Path: full, IsDir: true, ModTime: info.ModTime()})
			continue
		}
		if !Usable(name) {
			continue
		}
		files = append(files, Entry{Name: name, Path: full, Size: info.Size(), ModTime: info.ModTime()})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		if !files[i].ModTime.Equal(files[j].ModTime) {
			return files[i].ModTime.After(files[j].ModTime)
		}
		return files[i].Name < files[j].Name
	})

	entries := make([]Entry, 0, len(dirs)+len(files))
	entries = append(entries, dirs...)
	entries = append(entries, files...)
	return entries, nil
}
