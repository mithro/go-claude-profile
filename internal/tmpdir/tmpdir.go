// Package tmpdir checks and creates the directory Claude Code gets as its
// temp directory (CLAUDE_CODE_TMPDIR and TMPDIR), which must be on disk.
package tmpdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// tmpfsMagic is TMPFS_MAGIC from <linux/magic.h>.
const tmpfsMagic = 0x01021994

// MaxLen is the longest <dir>/claude-<uid> Claude Code accepts, as documented
// by the dot-claude hook-block-tmp-creation plugin.
const MaxLen = 44

// Check returns why dir is unsuitable as Claude's temp directory, or nil.
// dir need not exist yet; the nearest existing ancestor's filesystem counts.
func Check(dir string, uid int) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("temp directory %q is not an absolute path", dir)
	}
	if strings.ContainsAny(dir, " \t\n") {
		return fmt.Errorf("temp directory %q contains whitespace", dir)
	}
	sub := filepath.Join(dir, fmt.Sprintf("claude-%d", uid))
	if len(sub) > MaxLen {
		return fmt.Errorf("%s is %d bytes; Claude Code needs it within %d (shorten tmp_dir)", sub, len(sub), MaxLen)
	}
	probe := dir
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(probe, &st); err != nil {
		return fmt.Errorf("statfs %s: %w", probe, err)
	}
	if int64(st.Type) == tmpfsMagic {
		return fmt.Errorf("temp directory %s is on tmpfs (RAM); choose a directory on disk", dir)
	}
	return nil
}

// Ensure creates dir, mode 0700, if it does not exist.
func Ensure(dir string) error { return os.MkdirAll(dir, 0o700) }
