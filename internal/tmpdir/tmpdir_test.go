package tmpdir

import (
	"strings"
	"syscall"
	"testing"
)

func TestCheck(t *testing.T) {
	cases := map[string]struct {
		dir  string
		want string // "" means no error
	}{
		"relative":   {"rel/tmp", "not an absolute path"},
		"whitespace": {"/var/tmp/a b", "contains whitespace"},
		"too long":   {"/var/tmp/" + strings.Repeat("a", 30), "Claude Code needs it within 44"},
		"on disk":    {"/var/tmp/cp-check", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "on disk" && onTmpfs(t, "/var/tmp") {
				t.Skip("/var/tmp is tmpfs here")
			}
			err := Check(tc.dir, 1000)
			if tc.want == "" && err != nil {
				t.Fatalf("Check(%q) = %v", tc.dir, err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Check(%q) = %v, want it to contain %q", tc.dir, err, tc.want)
			}
		})
	}
}

func TestCheckRefusesTmpfs(t *testing.T) {
	if !onTmpfs(t, "/dev/shm") {
		t.Skip("/dev/shm is not tmpfs here")
	}
	if err := Check("/dev/shm/cp", 1000); err == nil || !strings.Contains(err.Error(), "tmpfs") {
		t.Fatalf("Check(/dev/shm/cp) = %v, want a tmpfs error", err)
	}
}

func onTmpfs(t *testing.T, p string) bool {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		t.Skipf("statfs %s: %v", p, err)
	}
	return int64(st.Type) == tmpfsMagic
}
