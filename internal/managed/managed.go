// Package managed writes the files claude-profile owns. A file it owns
// starts with Marker; any other file at the same path belongs to the user
// and is never changed.
package managed

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// Marker is the first line of every file claude-profile owns.
const Marker = "# Managed by claude-profile, which rewrites this file. Delete this line to keep your own edits."

// Result reports what Sync did, or would do, to each path.
type Result struct {
	Written, Unchanged, Foreign []string
}

// Changed reports whether any file was (or would be) written.
func (r Result) Changed() bool { return len(r.Written) > 0 }

// Owned reports whether a file's contents start with Marker.
func Owned(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte{'\n'})
	return string(first) == Marker
}

// Sync makes each file name in want hold its content, in dir. A file that
// exists without Marker is reported as Foreign and left alone. With dryRun,
// nothing is written.
func Sync(dir string, want map[string]string, dryRun bool) (Result, error) {
	var r Result
	for _, name := range slices.Sorted(maps.Keys(want)) {
		path := filepath.Join(dir, name)
		content := want[name]
		old, err := os.ReadFile(path)
		switch {
		case err == nil && string(old) == content:
			r.Unchanged = append(r.Unchanged, path)
			continue
		case err == nil && !Owned(old):
			r.Foreign = append(r.Foreign, path)
			continue
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return r, err
		}
		if !dryRun {
			if err := write(path, content); err != nil {
				return r, err
			}
		}
		r.Written = append(r.Written, path)
	}
	return r, nil
}

// Remove deletes the files in dir matching pattern that start with Marker.
func Remove(dir, pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil || !Owned(data) {
			continue
		}
		if err := os.Remove(m); err != nil {
			return removed, err
		}
		removed = append(removed, m)
	}
	return removed, nil
}

// write replaces path atomically.
func write(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".claude-profile-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
