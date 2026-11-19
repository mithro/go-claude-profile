package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func base() Request {
	return Request{
		Env:        map[string]string{"HOME": "/home/u", "PATH": "/usr/bin", "CLAUDE_CONFIG_DIR": "/inherited"},
		Profile:    profile.Profile{Name: "west-corp", Dir: "/home/u/.claude-west-corp", Account: "w@example.com"},
		Args:       []string{"--resume", "abc"},
		Claude:     "/usr/bin/claude",
		SystemdRun: "/usr/bin/systemd-run",
		TmpDir:     "/home/u/.claude-tmp",
		Limits:     config.Limits{"TasksMax": "4096", "MemoryMax": "8G"},
	}
}

func envOf(p Plan) map[string]string {
	m := map[string]string{}
	for _, kv := range p.Env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func TestBuildScope(t *testing.T) {
	plan, err := Build(base())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/systemd-run", "--user", "--scope", "--collect", "--quiet", "--same-dir",
		`--slice=claude-west\x2dcorp.slice`, "--description=Claude west-corp",
		"-p", "MemoryMax=8G", "-p", "TasksMax=4096", "--", "/usr/bin/claude", "--resume", "abc"}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv:\n got %q\nwant %q", plan.Argv, want)
	}
	env := envOf(plan)
	for k, v := range map[string]string{
		"CLAUDE_CONFIG_DIR":  "/home/u/.claude-west-corp",
		"CLAUDE_PROFILE":     "west-corp",
		"CLAUDE_CODE_TMPDIR": "/home/u/.claude-tmp",
		"TMPDIR":             "/home/u/.claude-tmp",
		"PATH":               "/usr/bin",
		"HOME":               "/home/u",
	} {
		if env[k] != v {
			t.Errorf("env %s = %q, want %q", k, env[k], v)
		}
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("Warnings = %q", plan.Warnings)
	}
}

func TestBuildDefaultProfileUnsetsConfigDir(t *testing.T) {
	r := base()
	r.Profile = profile.Profile{Name: "default", Dir: "/home/u/.claude", Default: true}
	plan, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := envOf(plan)["CLAUDE_CONFIG_DIR"]; ok {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want it unset", v)
	}
}

func TestBuildConfig(t *testing.T) {
	r := base()
	r.Env["CLAUDE_CODE_SUBAGENT_MODEL"] = "haiku"
	r.ConfigName = "claude-ac-coordinator"
	r.Config = &config.Config{
		Profile:     "west-corp",
		Args:        []string{"--model", "fable", "--name", "ac-coord"},
		Env:         map[string]string{"AC_SESSION": "ac-coord"},
		EnvDefault:  map[string]string{"CLAUDE_CODE_SUBAGENT_MODEL": "opus", "OTHER": "x"},
		PathPrepend: []string{"/home/u/wrappers"},
	}
	plan, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	tail := plan.Argv[len(plan.Argv)-7:]
	if want := []string{"/usr/bin/claude", "--model", "fable", "--name", "ac-coord", "--resume", "abc"}; !reflect.DeepEqual(tail, want) {
		t.Errorf("claude argv = %q, want %q", tail, want)
	}
	if !strings.Contains(strings.Join(plan.Argv, " "), "--description=Claude west-corp [claude-ac-coordinator]") {
		t.Errorf("description missing from %q", plan.Argv)
	}
	env := envOf(plan)
	if env["AC_SESSION"] != "ac-coord" || env["CLAUDE_CODE_SUBAGENT_MODEL"] != "haiku" || env["OTHER"] != "x" {
		t.Errorf("env = %v", env)
	}
	if env["PATH"] != "/home/u/wrappers:/usr/bin" {
		t.Errorf("PATH = %q", env["PATH"])
	}
}

func TestBuildNameArg(t *testing.T) {
	cfg := &config.Config{Profile: "west-corp", NameArg: &config.NameArg{Env: "AC_SESSION", Prefix: "ac-"}}

	r := base()
	r.Config, r.ConfigName, r.Args = cfg, "claude-ac", []string{"issues", "--model", "opus"}
	plan, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	tail := plan.Argv[len(plan.Argv)-5:]
	if want := []string{"/usr/bin/claude", "--name", "ac-issues", "--model", "opus"}; !reflect.DeepEqual(tail, want) {
		t.Errorf("argv = %q, want %q", tail, want)
	}
	if got := envOf(plan)["AC_SESSION"]; got != "ac-issues" {
		t.Errorf("AC_SESSION = %q", got)
	}

	r = base()
	r.Config, r.ConfigName, r.Args = cfg, "claude-ac", []string{"--resume", "abc"}
	r.Env["AC_SESSION"] = "ac-already"
	plan, err = Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if tail := plan.Argv[len(plan.Argv)-3:]; !reflect.DeepEqual(tail, []string{"/usr/bin/claude", "--resume", "abc"}) {
		t.Errorf("argv with AC_SESSION set = %q", tail)
	}

	r = base()
	r.Config, r.ConfigName, r.Args = cfg, "claude-ac", []string{"--model", "opus"}
	if _, err := Build(r); !usage.Is(err) || !strings.Contains(err.Error(), "claude-ac needs a NAME") {
		t.Errorf("missing NAME: err = %v", err)
	}
}

func TestBuildRequireEnv(t *testing.T) {
	r := base()
	r.Config, r.ConfigName = &config.Config{Profile: "x", RequireEnv: []string{"BL_SESSION"}}, "claude-bl"
	if _, err := Build(r); !usage.Is(err) || !strings.Contains(err.Error(), "BL_SESSION") {
		t.Errorf("err = %v, want a usage error naming BL_SESSION", err)
	}
	r.Env["BL_SESSION"] = "bl-a"
	if _, err := Build(r); err != nil {
		t.Errorf("with BL_SESSION set: %v", err)
	}
}

func TestBuildDirect(t *testing.T) {
	r := base()
	r.SystemdRun = ""
	plan, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/bin/claude", "--resume", "abc"}; !reflect.DeepEqual(plan.Argv, want) {
		t.Errorf("Argv = %q, want %q", plan.Argv, want)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("Warnings = %q", plan.Warnings)
	}
	r.Env[NoCgroupEnv] = "1"
	plan, err = Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "without resource limits") {
		t.Errorf("Warnings = %q", plan.Warnings)
	}
}

func TestBuildMismatchWarning(t *testing.T) {
	r := base()
	r.Profile.Expected = "other@example.com"
	plan, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "logged in as w@example.com") {
		t.Errorf("Warnings = %q", plan.Warnings)
	}
}

func TestWantScope(t *testing.T) {
	inside := "0::/user.slice/user-1000.slice/user@1000.service/claude.slice/claude-work.slice/run-x.scope\n"
	if WantScope(map[string]string{}, inside, "work") {
		t.Error("WantScope inside the profile's slice = true")
	}
	if !WantScope(map[string]string{}, inside, "other") {
		t.Error("WantScope inside another profile's slice = false")
	}
	if WantScope(map[string]string{NoCgroupEnv: "1"}, "", "work") {
		t.Error("WantScope with the opt-out = true")
	}
}

func TestResolveClaude(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "claude-profile")
	selfDir := filepath.Join(root, "a")
	realDir := filepath.Join(root, "b")
	for _, d := range []string{selfDir, realDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(selfDir, "claude")); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "claude")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveClaude("", selfDir+":"+realDir, self)
	if err != nil || got != real {
		t.Errorf("ResolveClaude = %q, %v; want %q", got, err, real)
	}
	if _, err := ResolveClaude("", selfDir, self); err == nil {
		t.Error("ResolveClaude found only itself but returned no error")
	}
	if got, err := ResolveClaude(real, "", self); err != nil || got != real {
		t.Errorf("ResolveClaude(configured) = %q, %v", got, err)
	}
	if _, err := ResolveClaude(filepath.Join(root, "missing"), "", self); !usage.Is(err) {
		t.Errorf("ResolveClaude(missing) err = %v, want a usage error", err)
	}
}

func TestResolveClaudeRejectsConfiguredSelf(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "claude-profile")
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "claude")
	if err := os.Symlink(self, link); err != nil {
		t.Fatal(err)
	}
	for _, configured := range []string{self, link} {
		_, err := ResolveClaude(configured, "", self)
		if !usage.Is(err) || !strings.Contains(err.Error(), "is claude-profile itself") {
			t.Errorf("ResolveClaude(%s) err = %v, want a usage error naming claude-profile itself", configured, err)
		}
	}
}
