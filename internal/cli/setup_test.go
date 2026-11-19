package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var (
	enableTimer = []string{"systemctl", "--user", "enable", "--now", "systemd-tmpfiles-clean.timer"}
	timerActive = []string{"systemctl", "--user", "is-active", "--quiet", "systemd-tmpfiles-clean.timer"}
)

func TestSetupInstallStatusUninstall(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	f.config(`{"link_dir": "~/bin", "configs": {"claude-w": {"profile": "work"}}}`)

	if code := f.run("claude-profile", "setup", "status"); code != exitFail {
		t.Errorf("status before install: exit %d, want %d", code, exitFail)
	}
	if code := f.run("claude-profile", "setup", "install"); code != exitOK {
		t.Fatalf("install: exit %d: %s%s", code, f.out, f.errb)
	}
	units := filepath.Join(f.home, ".config", "systemd", "user")
	for _, n := range []string{"claude.slice", "claude-default.slice", "claude-work.slice"} {
		if _, err := os.Stat(filepath.Join(units, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.home, ".config", "user-tmpfiles.d", "claude-profile.conf")); err != nil {
		t.Errorf("tmpfiles rule: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(f.home, "bin", "claude-w")); err != nil || target != f.d.Self {
		t.Errorf("link claude-w -> %q, %v", target, err)
	}
	if !slices.ContainsFunc(f.runs, func(a []string) bool { return slices.Equal(a, reload) }) {
		t.Errorf("no daemon-reload in %q", f.runs)
	}

	if !slices.ContainsFunc(f.runs, func(a []string) bool { return slices.Equal(a, enableTimer) }) {
		t.Errorf("no timer enable in %q", f.runs)
	}

	f.runs = nil
	if code := f.run("claude-profile", "setup", "install"); code != exitOK {
		t.Errorf("second install: exit %d", code)
	}
	if slices.ContainsFunc(f.runs, func(a []string) bool { return slices.Equal(a, reload) }) {
		t.Errorf("second install repeated daemon-reload: %q", f.runs)
	}
	if !strings.Contains(f.out.String(), "enabled  systemd-tmpfiles-clean.timer") {
		t.Errorf("install output lacks timer report: %q", f.out)
	}
	f.runs = nil
	if code := f.run("claude-profile", "setup", "status"); code != exitOK || len(f.runs) != 0 {
		t.Errorf("status: exit %d, runs %q", code, f.runs)
	}
	if code := f.run("claude-profile", "setup", "status"); code != exitOK {
		t.Errorf("status after install: exit %d: %s", code, f.out)
	}

	if code := f.run("claude-profile", "setup", "uninstall"); code != exitOK {
		t.Fatalf("uninstall: exit %d: %s", code, f.errb)
	}
	if _, err := os.Stat(filepath.Join(units, "claude.slice")); !os.IsNotExist(err) {
		t.Errorf("claude.slice still there: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.home, "bin", "claude-w")); !os.IsNotExist(err) {
		t.Errorf("link still there: %v", err)
	}
}

func TestSetupInstallFailsWhenTimerEnableFails(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.d.Run = func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
		f.runs = append(f.runs, argv)
		if slices.Equal(argv, enableTimer) {
			return errors.New("boom")
		}
		return nil
	}
	if code := f.run("claude-profile", "setup", "install"); code != exitFail {
		t.Errorf("exit %d, want %d", code, exitFail)
	}
	if !strings.Contains(f.errb.String(), "systemd-tmpfiles-clean.timer") {
		t.Errorf("stderr = %q", f.errb)
	}
}

func TestSetupReportsForeignFiles(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.write(filepath.Join(f.home, ".config", "systemd", "user", "claude.slice"), "[Slice]\n", 0o644)
	if code := f.run("claude-profile", "setup", "install"); code != exitFail {
		t.Errorf("exit %d, want %d", code, exitFail)
	}
	if !strings.Contains(f.out.String(), "not managed") {
		t.Errorf("output = %q", f.out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(f.home, ".config", "systemd", "user", "claude.slice")); string(data) != "[Slice]\n" {
		t.Error("a foreign unit was overwritten")
	}
}

func TestDoctor(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	f.write(filepath.Join(f.root, "cgroup", "user.slice", "user-1000.slice", "user@1000.service", "cgroup.controllers"), "cpu memory pids\n", 0o644)
	f.config(`{"profiles": {"work": {"account": "c@example.com"}}, "configs": {"claude-w": {"profile": "work"}}}`)
	f.run("claude-profile", "setup", "install")

	if code := f.run("claude-profile", "doctor"); code != exitOK {
		t.Fatalf("doctor: exit %d:\n%s", code, f.out)
	}
	f.config(`{"profiles": {"work": {"account": "w@example.com"}}, "configs": {"claude-w": {"profile": "gone"}}}`)
	if code := f.run("claude-profile", "doctor"); code != exitFail {
		t.Fatalf("doctor with problems: exit %d", code)
	}
	for _, want := range []string{"FAIL  profile work: logged in as c@example.com, expected w@example.com", `FAIL  config claude-w: names profile "gone"`} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, f.out)
		}
	}
}

func TestDoctorFailsWhenTmpfilesTimerInactive(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.write(filepath.Join(f.root, "cgroup", "user.slice", "user-1000.slice", "user@1000.service", "cgroup.controllers"), "cpu memory pids\n", 0o644)
	f.run("claude-profile", "setup", "install")
	if code := f.run("claude-profile", "doctor"); code != exitOK {
		t.Fatalf("doctor: exit %d:\n%s", code, f.out)
	}
	if !slices.ContainsFunc(f.runs, func(a []string) bool { return slices.Equal(a, timerActive) }) {
		t.Errorf("no is-active check in %q", f.runs)
	}
	f.d.Run = func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
		if slices.Equal(argv, timerActive) {
			return errors.New("inactive")
		}
		return nil
	}
	if code := f.run("claude-profile", "doctor"); code != exitFail {
		t.Fatalf("doctor: exit %d, want %d", code, exitFail)
	}
	for _, want := range []string{"FAIL  tmpfiles cleanup timer", "run: systemctl --user enable --now systemd-tmpfiles-clean.timer"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, f.out)
		}
	}
}
