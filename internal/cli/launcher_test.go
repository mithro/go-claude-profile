package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func envOf(env []string) map[string]string { return envMap(env) }

var reload = []string{"systemctl", "--user", "daemon-reload"}

func TestConfigSymlinkLaunchesInScope(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	ws := f.profile("west-corp", "c@example.com")
	f.config(`{"configs": {"claude-ac-coordinator": {"profile": "west-corp",
		"args": ["--model", "fable", "--name", "ac-coord"],
		"env": {"AC_SESSION": "ac-coord"}}}}`)

	if code := f.run("/home/u/bin/claude-ac-coordinator", "hello"); code != exitFail || len(f.execs) != 1 {
		t.Fatalf("exit %d, %d execs; stderr %s", code, len(f.execs), f.errb)
	}
	want := []string{"/usr/bin/systemd-run", "--user", "--scope", "--collect", "--quiet", "--same-dir",
		`--slice=claude-west\x2dcorp.slice`, "--description=Claude west-corp [claude-ac-coordinator]",
		"-p", "MemoryHigh=6G", "-p", "MemoryMax=8G", "-p", "MemorySwapMax=2G", "-p", "TasksMax=4096",
		"--", filepath.Join(f.bin, "claude"), "--model", "fable", "--name", "ac-coord", "hello"}
	if got := f.execs[0].argv; !reflect.DeepEqual(got, want) {
		t.Errorf("argv:\n got %q\nwant %q", got, want)
	}
	env := envOf(f.execs[0].env)
	if env["CLAUDE_CONFIG_DIR"] != ws || env["AC_SESSION"] != "ac-coord" || env["TMPDIR"] != filepath.Join(f.home, ".claude-tmp") {
		t.Errorf("env = %v", env)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".claude-tmp")); err != nil {
		t.Errorf("temp dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".config", "systemd", "user", `claude-west\x2dcorp.slice`)); err != nil {
		t.Errorf("slice unit not written: %v", err)
	}
	if !slices.ContainsFunc(f.runs, func(a []string) bool { return slices.Equal(a, reload) }) {
		t.Errorf("no daemon-reload in %q", f.runs)
	}
	f.runs = nil
	f.run("/home/u/bin/claude-ac-coordinator", "hello")
	if len(f.runs) != 0 {
		t.Errorf("second launch ran %q, want nothing (units unchanged)", f.runs)
	}
}

func TestResumeSelectsOwningProfile(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(work, "projects", "-src", sid+".jsonl"), "{}\n", 0o644)

	f.run("claude-launch", "--resume", sid)
	if len(f.execs) != 1 {
		t.Fatalf("no exec; stderr %s", f.errb)
	}
	if got := envOf(f.execs[0].env)["CLAUDE_CONFIG_DIR"]; got != work {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, work)
	}
}

func TestResumeConflictsWithExplicitProfile(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(work, "projects", "-src", sid+".jsonl"), "{}\n", 0o644)

	if code := f.run("claude-profile", "run", "--profile", "default", "--", "--resume", sid); code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(f.errb.String(), "belongs to profile work, not default") {
		t.Errorf("stderr = %q", f.errb.String())
	}
}

func TestNoSelectionWithoutTerminal(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	if code := f.run("claude-launch"); code != exitUsage {
		t.Fatalf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(f.errb.String(), "profiles: default, work") {
		t.Errorf("stderr = %q", f.errb.String())
	}
}

func TestMenuChoiceIsRemembered(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "w@example.com")
	// 1 default, 2 new for me@, 3 work, 4 new for w@, 5 new.
	f.tty = "3\n"
	f.run("claude-launch")
	if len(f.execs) != 1 || envOf(f.execs[0].env)["CLAUDE_CONFIG_DIR"] != work {
		t.Fatalf("execs = %+v; stderr %s", f.execs, f.errb)
	}
	f.tty = "\n"
	f.run("claude-launch")
	if len(f.execs) != 2 || envOf(f.execs[1].env)["CLAUDE_CONFIG_DIR"] != work {
		t.Errorf("Enter did not repeat the last choice; stderr %s", f.errb)
	}
}

func TestInheritedProfile(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.d.Env["CLAUDE_PROFILE"] = "work"
	f.run("claude-launch")
	if len(f.execs) != 1 || envOf(f.execs[0].env)["CLAUDE_CONFIG_DIR"] != work {
		t.Errorf("execs = %+v; stderr %s", f.execs, f.errb)
	}
}

func TestInsideSliceExecsClaudeDirectly(t *testing.T) {
	f := newFixture(t)
	f.profile("work", "c@example.com")
	f.d.Env["CLAUDE_PROFILE"] = "work"
	f.write(filepath.Join(f.root, "proc", "self", "cgroup"),
		"0::/user.slice/user-1000.slice/user@1000.service/claude.slice/claude-work.slice/run-p1-i1.scope\n", 0o644)
	f.run("claude-launch", "--continue")
	if len(f.execs) != 1 {
		t.Fatalf("no exec; stderr %s", f.errb)
	}
	if want := []string{filepath.Join(f.bin, "claude"), "--continue"}; !reflect.DeepEqual(f.execs[0].argv, want) {
		t.Errorf("argv = %q, want %q", f.execs[0].argv, want)
	}
	if len(f.runs) != 0 {
		t.Errorf("runs = %q, want none", f.runs)
	}
}

func TestNewProfile(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	var loginArgv []string
	f.d.Run = func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
		loginArgv = argv
		dir := envOf(env)["CLAUDE_CONFIG_DIR"]
		return os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount": {"emailAddress": "w2@example.com"}}`), 0o600)
	}
	if code := f.run("claude-profile", "new", "--account", "w2@example.com", "fresh"); code != exitOK {
		t.Fatalf("exit %d: %s", code, f.errb)
	}
	want := []string{filepath.Join(f.bin, "claude"), "auth", "login", "--claudeai", "--email", "w2@example.com"}
	if !reflect.DeepEqual(loginArgv, want) {
		t.Errorf("login argv = %q, want %q", loginArgv, want)
	}
	fi, err := os.Stat(filepath.Join(f.home, ".claude-fresh"))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("profile dir: %v, %v", fi, err)
	}
	if !strings.Contains(f.errb.String(), "logged in as w2@example.com") {
		t.Errorf("stderr = %q", f.errb.String())
	}
}

func TestNewProfileLoginFails(t *testing.T) {
	f := newFixture(t)
	f.d.Run = func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
		return errors.New("exit status 1")
	}
	if code := f.run("claude-profile", "new", "fresh"); code != exitFail {
		t.Fatalf("exit %d, want %d", code, exitFail)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".claude-fresh")); !os.IsNotExist(err) {
		t.Errorf("the empty profile directory was left behind: %v", err)
	}
}
