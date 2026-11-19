//go:build systemdlive

package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/slice"
)

// TestLiveScope starts a fake claude through the real systemd user manager
// and checks, from inside, that it landed in the profile's slice with the
// requested limits. It stops only the throwaway profile slice it created,
// never claude.slice, which may hold real sessions.
func TestLiveScope(t *testing.T) {
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		t.Skip("no systemd-run")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"cg=$(sed -n 's/^0:://p' /proc/self/cgroup)\n" +
		"{ echo \"$cg\"; cat \"/sys/fs/cgroup$cg/memory.max\"; cat \"/sys/fs/cgroup$cg/pids.max\"; echo \"$CLAUDE_PROFILE\"; echo \"$*\"; } > \"$OUT\"\n" +
		"exit 3\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("live%d", os.Getpid())
	t.Cleanup(func() {
		cmd := exec.Command("systemctl", "--user", "stop", slice.UnitName(name))
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Logf("stopping %s: %v", slice.UnitName(name), err)
		}
	})

	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	env["OUT"] = out
	plan, err := Build(Request{
		Env:        env,
		Profile:    profile.Profile{Name: name, Dir: dir},
		Args:       []string{"--resume", "x"},
		Claude:     fake,
		SystemdRun: run,
		TmpDir:     dir,
		Limits:     config.Limits{"MemoryMax": "300M", "TasksMax": "77"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(plan.Argv[0], plan.Argv[1:]...)
	cmd.Env = plan.Env
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("run: %v, want exit status 3 passed through", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("output = %q", data)
	}
	if want := "/" + slice.Top + "/" + slice.UnitName(name) + "/"; !strings.Contains(lines[0], want) {
		t.Errorf("cgroup %q is not under %q", lines[0], want)
	}
	if lines[1] != "314572800" || lines[2] != "77" || lines[3] != name || lines[4] != "--resume x" {
		t.Errorf("memory.max %q, pids.max %q, CLAUDE_PROFILE %q, args %q", lines[1], lines[2], lines[3], lines[4])
	}
}
