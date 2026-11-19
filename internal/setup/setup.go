// Package setup installs claude-profile's per-user files other than the
// slice units: the tmpfiles.d rule that ages out the temp directory, and the
// symlinks that make each config a command.
package setup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mithro/go-claude-profile/internal/managed"
)

// TmpfilesName is the tmpfiles.d rule's file name.
const TmpfilesName = "claude-profile.conf"

// TmpfilesDir is the user's tmpfiles.d directory.
func TmpfilesDir(env map[string]string, home string) string {
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return filepath.Join(x, "user-tmpfiles.d")
	}
	return filepath.Join(home, ".config", "user-tmpfiles.d")
}

// TmpfilesContent creates the temp directory and removes entries in it
// untouched for 14 days.
func TmpfilesContent(tmpDir string) string {
	return managed.Marker + "\nd " + tmpDir + " 0700 - - 14d\n"
}

// LinkResult reports what SyncLinks did, or would do, to each path.
type LinkResult struct {
	Created, Unchanged, Foreign []string
}

// SyncLinks makes dir/<name> a symlink to self for each name. A path that
// exists and is not a symlink to self is reported as Foreign and left alone.
func SyncLinks(dir, self string, names []string, dryRun bool) (LinkResult, error) {
	var r LinkResult
	for _, n := range names {
		path := filepath.Join(dir, n)
		fi, err := os.Lstat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if !dryRun {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return r, err
				}
				if err := os.Symlink(self, path); err != nil {
					return r, err
				}
			}
			r.Created = append(r.Created, path)
		case err != nil:
			return r, err
		case fi.Mode()&fs.ModeSymlink != 0 && pointsAt(path, self):
			r.Unchanged = append(r.Unchanged, path)
		default:
			r.Foreign = append(r.Foreign, path)
		}
	}
	return r, nil
}

// RemoveLinks removes dir/<name> for each name that is a symlink to self.
func RemoveLinks(dir, self string, names []string) ([]string, error) {
	var removed []string
	for _, n := range names {
		path := filepath.Join(dir, n)
		fi, err := os.Lstat(path)
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 || !pointsAt(path, self) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	return removed, nil
}

func pointsAt(path, self string) bool {
	r, err := filepath.EvalSymlinks(path)
	return err == nil && r == self
}
