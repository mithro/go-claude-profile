# claude-profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `claude-profile`, a small Go launcher that starts Claude Code in the right config directory ("profile") for an account, applies named launch presets ("configs"), runs Claude inside a resource-limited systemd user scope, and points its temp files at disk; package it as a Debian package published to a signed apt repository, in a new GitHub repository set up like `go-tmux-saver`.

**Architecture:** One static binary, dispatched on the name it is invoked as (`claude-profile` is the management command; any other name, normally a symlink, is a launcher). Library packages under `internal/` each take their inputs (home directory, environment, readers, writers, exec functions) as parameters; only `internal/cli` touches the real process. A launch builds an argument list for `systemd-run --user --scope` and replaces the process with it.

**Tech Stack:** Go (standard library only), systemd user manager (cgroup v2), nfpm, GitHub Actions with `mithro/apt-repo-action`.

**Spec:** `docs/superpowers/specs/2026-11-19-claude-profile-design.md` (read it before starting; this plan implements it).

## Global Constraints

- Module `github.com/mithro/go-claude-profile`, `go 1.26.6` in `go.mod`, Linux only, **no third-party dependencies** (no `go.sum`).
- Binary and Debian package name: `claude-profile`. Repository: `mithro/go-claude-profile`, public, Apache-2.0.
- Exit codes: `0` success, `1` failure, `2` usage or configuration error; a launch replaces the process with Claude, so Claude's own status is returned.
- Errors print as `claude-profile: <message>` on stderr; warnings as `claude-profile: warning: <message>`.
- Environment variables the tool sets or reads: `CLAUDE_CONFIG_DIR`, `CLAUDE_PROFILE`, `CLAUDE_PROFILE_NO_CGROUP`, `CLAUDE_CODE_TMPDIR`, `TMPDIR`.
- Tool config: `$XDG_CONFIG_HOME/claude-profile/config.json` (default `~/.config/claude-profile/config.json`); state: `$XDG_STATE_HOME/claude-profile/last.json` (default `~/.local/state/...`).
- Default session limits: `MemoryHigh=6G`, `MemoryMax=8G`, `MemorySwapMax=2G`, `TasksMax=4096`. Allowed limit names: `CPUQuota`, `CPUWeight`, `MemoryHigh`, `MemoryMax`, `MemorySwapMax`, `TasksMax`.
- Default `tmp_dir` `~/.claude-tmp`; `<tmp_dir>/claude-<uid>` must be at most 44 bytes and not on tmpfs.
- Managed-file marker (first line of every file the tool owns): `# Managed by claude-profile, which rewrites this file. Delete this line to keep your own edits.`
- **Never write to `/tmp`.** Run tests only through `make test` / `make test-live`, which set `TMPDIR` and `GOTMPDIR` to `./tmp`. Never redirect stderr to `/dev/null`.
- Run `gofmt -w` on every Go file you create or change before running the tests; `make fmt-check` (and CI) fail on unformatted code. The code in this plan is correct but its column alignment is not guaranteed to be gofmt's.
- If code in this plan fails to compile or a test in it is wrong, fix it in the obvious minimal way, keep the behaviour the spec and the task describe, and say what you changed in the task's commit message.
- Work happens in the worktree `~/github/mithro/go-claude-profile/.worktrees/initial` on branch `initial` (created in Task 1). Commit at the end of every task; every commit message ends with:

  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF
  ```

  (If the executing model differs, use its own name in the `Co-Authored-By` line.)
- Never push, create the GitHub repository, or touch anything outside the worktree before Task 14, and in Task 14/15 only where the step says so.

## File Structure

```
go.mod                               module, Go version
Makefile                             test (TMPDIR=./tmp), test-live, vet, fmt-check, build
LICENSE                              Apache-2.0 (copied from go-tmux-saver)
README.md                            user documentation, Install section per apt-repo-action
nfpm.yaml                            Debian package definition
.github/apt-packaging.toml           kind = "B"
.github/workflows/deb.yml            test, per-arch build, apt publish, GitHub Release
packaging/release-assets.sh          release asset naming (copied from go-tmux-saver)
packaging/release-assets-test.sh     its test (copied)
cmd/claude-profile/main.go           os.Exit(cli.RunMultiCall(...))
internal/usage/usage.go              usage-error type (exit 2)
internal/config/config.go            load + validate config.json, limits merge, ~ expansion
internal/profile/profile.go          discovery, account detection, Create, Find, ByDir
internal/resume/resume.go            --resume scanning, session lookup by UUID or name
internal/managed/managed.go          write/remove files carrying the marker
internal/slice/slice.go              slice unit names, escaping, rendering, InSlice
internal/tmpdir/tmpdir.go            temp dir checks (tmpfs, length) and creation
internal/launch/launch.go            environment + systemd-run argv, ResolveClaude
internal/launch/live_test.go         systemdlive: real scope, read back limits
internal/menu/menu.go                grouped numbered menu, prompts, last choice
internal/setup/setup.go              tmpfiles rule, config symlinks
internal/cli/cli.go                  Deps, dispatch, help, shared helpers
internal/cli/list.go                 list [--json]
internal/cli/which.go                which [--json] <uuid|name|pid>
internal/cli/launcher.go             launcher flow, run, new
internal/cli/setup.go                setup install|status|uninstall
internal/cli/doctor.go               doctor
```

---

### Task 1: Repository scaffold, usage errors and the version command

**Files:**
- Create: `go.mod`, `Makefile`, `LICENSE`, `cmd/claude-profile/main.go`, `internal/usage/usage.go`, `internal/usage/usage_test.go`, `internal/cli/cli.go`, `internal/cli/launcher.go`, `internal/cli/helpers_test.go`, `internal/cli/cli_test.go`

**Interfaces:**
- Produces: `usage.Errorf(format, ...any) error`, `usage.Is(error) bool`; `cli.RunMultiCall(argv0 string, args []string, d Deps) int`; `cli.OSDeps() Deps`; `cli.Version`; unexported `register(command)`, `fail(Deps, error) int`, `warn(Deps, string, ...any)`, constants `exitOK`, `exitFail`, `exitUsage`, `toolName`. (Task 9 replaces `Deps`, `OSDeps` and the test helpers with fuller versions.)

- [ ] **Step 1: Create the worktree**

The repository already exists locally at `~/github/mithro/go-claude-profile` (branch `main` holds only `.gitignore`; branch `design-spec` holds the spec and this plan). Use the `superpowers:using-git-worktrees` skill, then:

```bash
cd ~/github/mithro/go-claude-profile
git worktree add .worktrees/initial -b initial design-spec
cd .worktrees/initial
```

All later paths are relative to this worktree.

- [ ] **Step 2: Write `go.mod`, `Makefile` and `LICENSE`**

`go.mod`:

```
module github.com/mithro/go-claude-profile

go 1.26.6
```

`Makefile`:

```make
# claude-profile developer tasks.
#
# Tests keep their temporary files under ./tmp, never the RAM-backed /tmp.

GO ?= go
export TMPDIR := $(CURDIR)/tmp
export GOTMPDIR := $(CURDIR)/tmp

.PHONY: test test-live vet fmt-check build

test: | tmp
	$(GO) test -race ./...

# Starts real systemd user scopes (needs a running user manager).
test-live: | tmp
	$(GO) test -race -tags systemdlive -count=1 ./internal/launch/

vet:
	$(GO) vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

build: | tmp
	$(GO) build -o claude-profile ./cmd/claude-profile

tmp:
	mkdir -p tmp
```

`LICENSE`: copy go-tmux-saver's Apache-2.0 text verbatim:

```bash
git -C ~/github/mithro/go-tmux-saver show origin/main:LICENSE > LICENSE
head -3 LICENSE
```

Expected: the first lines of the Apache License 2.0.

- [ ] **Step 3: Write the failing tests**

`internal/usage/usage_test.go`:

```go
package usage

import (
	"errors"
	"fmt"
	"testing"
)

func TestIs(t *testing.T) {
	u := Errorf("bad %s", "flag")
	if u.Error() != "bad flag" {
		t.Errorf("message = %q", u.Error())
	}
	if !Is(u) {
		t.Error("Is(usage error) = false")
	}
	if !Is(fmt.Errorf("wrapped: %w", u)) {
		t.Error("Is(wrapped usage error) = false")
	}
	if Is(errors.New("plain")) {
		t.Error("Is(plain error) = true")
	}
	if Is(nil) {
		t.Error("Is(nil) = true")
	}
}
```

`internal/cli/helpers_test.go`:

```go
package cli

import (
	"bytes"
	"testing"
)

// testDeps is replaced by a fuller fixture in Task 9.
func testDeps(t *testing.T) (Deps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	return Deps{Env: map[string]string{}, Stdout: &out, Stderr: &errb}, &out, &errb
}
```

`internal/cli/cli_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		d, out, _ := testDeps(t)
		if got := RunMultiCall("/usr/bin/claude-profile", []string{arg}, d); got != exitOK {
			t.Fatalf("%s: exit %d", arg, got)
		}
		if got, want := out.String(), "claude-profile dev\n"; got != want {
			t.Errorf("%s: got %q, want %q", arg, got, want)
		}
	}
}

func TestNoArgumentsIsUsage(t *testing.T) {
	d, _, errb := testDeps(t)
	if got := RunMultiCall("claude-profile", nil, d); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(errb.String(), "usage: claude-profile") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	d, _, errb := testDeps(t)
	if got := RunMultiCall("claude-profile", []string{"frob"}, d); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if want := `claude-profile: unknown command "frob"`; !strings.Contains(errb.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errb.String(), want)
	}
}
```

- [ ] **Step 4: Run the tests to see them fail**

Run: `make test`
Expected: FAIL — the packages do not compile (`undefined: Errorf`, `undefined: RunMultiCall`).

- [ ] **Step 5: Write the implementation**

`internal/usage/usage.go`:

```go
// Package usage marks errors that are the caller's mistake: a bad argument,
// configuration or selection. The command line exits with status 2 for them.
package usage

import (
	"errors"
	"fmt"
)

// Error is a usage error.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

// Errorf formats a usage error.
func Errorf(format string, a ...any) error { return &Error{msg: fmt.Sprintf(format, a...)} }

// Is reports whether err is, or wraps, a usage error.
func Is(err error) bool {
	var u *Error
	return errors.As(err, &u)
}
```

`internal/cli/cli.go`:

```go
// Package cli is claude-profile's command line. It is the only package that
// reads the process environment, arguments and terminal; the rest of the
// program takes them as parameters so it can be tested in isolation.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mithro/go-claude-profile/internal/usage"
)

// Version is set at build time with
// -ldflags "-X github.com/mithro/go-claude-profile/internal/cli.Version=...".
var Version = "dev"

const toolName = "claude-profile"

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// Deps is everything the program touches outside its own memory.
type Deps struct {
	Env    map[string]string
	Stdout io.Writer
	Stderr io.Writer
}

// OSDeps returns the real process's dependencies.
func OSDeps() Deps {
	return Deps{Env: envMap(os.Environ()), Stdout: os.Stdout, Stderr: os.Stderr}
}

func envMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// RunMultiCall runs the program as invoked under argv0. Under its own name it
// is the management command; under any other name it is a launcher.
func RunMultiCall(argv0 string, args []string, d Deps) int {
	if name := filepath.Base(argv0); name != toolName {
		return runLauncher(name, args, d)
	}
	return runManagement(args, d)
}

// command is one management subcommand.
type command struct {
	name    string
	summary string
	run     func(args []string, d Deps) int
}

var commands []command

func register(c command) { commands = append(commands, c) }

func runManagement(args []string, d Deps) int {
	if len(args) == 0 {
		printHelp(d.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(d.Stdout, "%s %s\n", toolName, Version)
		return exitOK
	case "help", "--help", "-h":
		printHelp(d.Stdout)
		return exitOK
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:], d)
		}
	}
	return fail(d, usage.Errorf("unknown command %q (try %s help)", args[0], toolName))
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "usage: %s <command> [args]\n\n", toolName)
	fmt.Fprint(w, "Launches Claude Code in a chosen profile (config directory), inside a\n"+
		"resource-limited cgroup. Invoked under another name (a symlink), it is a\n"+
		"launcher: that name selects a config, and every argument goes to Claude.\n\n"+
		"commands:\n")
	cs := append([]command(nil), commands...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, c := range cs {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "  %-10s %s\n  %-10s %s\n", "help", "show this help", "version", "print the version")
}

// fail prints err and returns the exit status for it.
func fail(d Deps, err error) int {
	fmt.Fprintf(d.Stderr, "%s: %v\n", toolName, err)
	if usage.Is(err) {
		return exitUsage
	}
	return exitFail
}

func warn(d Deps, format string, a ...any) {
	fmt.Fprintf(d.Stderr, "%s: warning: %s\n", toolName, fmt.Sprintf(format, a...))
}
```

`internal/cli/launcher.go` (temporary; Task 10 replaces this file):

```go
package cli

import "github.com/mithro/go-claude-profile/internal/usage"

// runLauncher is replaced by the real launcher in Task 10.
func runLauncher(name string, args []string, d Deps) int {
	return fail(d, usage.Errorf("%s: the launcher is not built yet", name))
}
```

`cmd/claude-profile/main.go`:

```go
// claude-profile launches Claude Code under the right account and config
// directory, inside a resource-limited cgroup. It is Linux-only: it relies on
// systemd user scopes, cgroup v2 and /proc.

//go:build linux

package main

import (
	"os"

	"github.com/mithro/go-claude-profile/internal/cli"
)

func main() { os.Exit(cli.RunMultiCall(os.Args[0], os.Args[1:], cli.OSDeps())) }
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `make test && make vet && make fmt-check && make build && ./claude-profile version`
Expected: `ok` for `internal/usage` and `internal/cli`, then `claude-profile dev`.

- [ ] **Step 7: Commit**

```bash
rm claude-profile
git add go.mod Makefile LICENSE cmd internal
git commit -m "Scaffold the claude-profile command

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 2: The tool config file

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Limits map[string]string`
  - `type NameArg struct{ Env, Prefix string }`
  - `type Profile struct{ Account string; SliceLimits, SessionLimits Limits }`
  - `type Config struct{ Profile string; Args []string; Env, EnvDefault map[string]string; PathPrepend, RequireEnv []string; NameArg *NameArg; SessionLimits Limits }`
  - `type File struct{ Accounts []string; ClaudePath, TmpDir, LinkDir string; IgnoreProfiles []string; ExtraProfiles map[string]string; SessionLimits Limits; Profiles map[string]Profile; Configs map[string]Config }`
  - `func Path(env map[string]string, home string) string`
  - `func Load(path, home string) (*File, error)` (missing file → defaults; paths `~`-expanded; session limits merged over defaults)
  - `func (f *File) SessionLimitsFor(profileName, configName string) Limits`
  - `func (f *File) ConfigNames() []string` (sorted)
  - `func ValidProfileName(name string) error`
  - `func Expand(home, p string) string`
  - `func DefaultSessionLimits() Limits`

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "absent.json"), "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if f.TmpDir != "/home/u/.claude-tmp" {
		t.Errorf("TmpDir = %q", f.TmpDir)
	}
	if f.LinkDir != "/home/u/bin" {
		t.Errorf("LinkDir = %q", f.LinkDir)
	}
	if !reflect.DeepEqual(f.SessionLimits, DefaultSessionLimits()) {
		t.Errorf("SessionLimits = %v", f.SessionLimits)
	}
}

func TestLoadExpandsAndMerges(t *testing.T) {
	path := writeConfig(t, `{
		"tmp_dir": "~/scratch-tmp",
		"session_limits": {"MemoryMax": "4G"},
		"extra_profiles": {"work": "~/elsewhere/claude"},
		"profiles": {"work": {"account": "a@example.com",
			"slice_limits": {"MemoryMax": "20G"}, "session_limits": {"TasksMax": "100"}}},
		"configs": {"claude-w": {"profile": "work", "args": ["--model", "opus"],
			"path_prepend": ["~/bin/wrappers"], "session_limits": {"CPUQuota": "200%"}}}
	}`)
	f, err := Load(path, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if f.TmpDir != "/home/u/scratch-tmp" {
		t.Errorf("TmpDir = %q", f.TmpDir)
	}
	if got := f.ExtraProfiles["work"]; got != "/home/u/elsewhere/claude" {
		t.Errorf("ExtraProfiles[work] = %q", got)
	}
	if got := f.Configs["claude-w"].PathPrepend; !reflect.DeepEqual(got, []string{"/home/u/bin/wrappers"}) {
		t.Errorf("PathPrepend = %q", got)
	}
	want := Limits{"MemoryHigh": "6G", "MemoryMax": "4G", "MemorySwapMax": "2G", "TasksMax": "4096"}
	if !reflect.DeepEqual(f.SessionLimits, want) {
		t.Errorf("SessionLimits = %v, want %v", f.SessionLimits, want)
	}
	got := f.SessionLimitsFor("work", "claude-w")
	want = Limits{"MemoryHigh": "6G", "MemoryMax": "4G", "MemorySwapMax": "2G", "TasksMax": "100", "CPUQuota": "200%"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionLimitsFor(work, claude-w) = %v, want %v", got, want)
	}
	if got := f.SessionLimitsFor("other", ""); !reflect.DeepEqual(got, f.SessionLimits) {
		t.Errorf("SessionLimitsFor(other) = %v", got)
	}
	if got := f.Profiles["work"].SliceLimits["MemoryMax"]; got != "20G" {
		t.Errorf("slice MemoryMax = %q", got)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"unknown key":       {`{"acounts": []}`, `unknown field "acounts"`},
		"limit name":        {`{"session_limits": {"MemoryLimit": "1G"}}`, `"MemoryLimit" is not one of`},
		"limit value":       {`{"session_limits": {"MemoryMax": "1 G"}}`, "invalid value"},
		"profile name":      {`{"profiles": {"Work": {}}}`, `invalid profile name "Work"`},
		"reserved name":     {`{"profiles": {"tmp": {}}}`, `"tmp" is reserved`},
		"extra default":     {`{"extra_profiles": {"default": "/x"}}`, "cannot be redefined"},
		"config name":       {`{"configs": {"claude-profile": {"profile": "default"}}}`, "invalid config name"},
		"config no profile": {`{"configs": {"c": {}}}`, "profile is required"},
		"name_arg env":      {`{"configs": {"c": {"profile": "default", "name_arg": {"prefix": "x-"}}}}`, "name_arg.env is required"},
		"env name":          {`{"configs": {"c": {"profile": "default", "env": {"A=B": "x"}}}}`, "invalid environment variable name"},
		"slice limit":       {`{"profiles": {"w": {"slice_limits": {"Nice": "5"}}}}`, `"Nice" is not one of`},
		"bad json":          {`{`, "unexpected EOF"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.body), "/home/u")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestPath(t *testing.T) {
	if got := Path(map[string]string{}, "/home/u"); got != "/home/u/.config/claude-profile/config.json" {
		t.Errorf("default Path = %q", got)
	}
	if got := Path(map[string]string{"XDG_CONFIG_HOME": "/x"}, "/home/u"); got != "/x/claude-profile/config.json" {
		t.Errorf("XDG Path = %q", got)
	}
}

func TestConfigNamesSorted(t *testing.T) {
	f := &File{Configs: map[string]Config{"b": {}, "a": {}, "c": {}}}
	if got := f.ConfigNames(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("ConfigNames = %q", got)
	}
}

func TestExpand(t *testing.T) {
	for in, want := range map[string]string{"~": "/h", "~/a/b": "/h/a/b", "/abs": "/abs", "rel": "rel", "~x": "~x"} {
		if got := Expand("/h", in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/config` does not compile (`undefined: Load`).

- [ ] **Step 3: Write the implementation**

`internal/config/config.go`:

```go
// Package config loads claude-profile's own configuration file,
// ~/.config/claude-profile/config.json. A missing file means all defaults.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Limits are systemd resource-control properties, by name.
type Limits map[string]string

var allowedLimits = []string{"CPUQuota", "CPUWeight", "MemoryHigh", "MemoryMax", "MemorySwapMax", "TasksMax"}

const (
	DefaultTmpDir  = "~/.claude-tmp"
	DefaultLinkDir = "~/bin"
)

// DefaultSessionLimits are the per-session limits when config.json sets none.
func DefaultSessionLimits() Limits {
	return Limits{"MemoryHigh": "6G", "MemoryMax": "8G", "MemorySwapMax": "2G", "TasksMax": "4096"}
}

// NameArg reproduces `claude-ac NAME`: when Env is unset, the first argument
// is taken as NAME, Env is set to Prefix+NAME and `--name Prefix+NAME` is
// passed to Claude.
type NameArg struct {
	Env    string `json:"env"`
	Prefix string `json:"prefix"`
}

// Profile is per-profile configuration.
type Profile struct {
	Account       string `json:"account"`
	SliceLimits   Limits `json:"slice_limits"`
	SessionLimits Limits `json:"session_limits"`
}

// Config is a named set of launch arguments.
type Config struct {
	Profile       string            `json:"profile"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	EnvDefault    map[string]string `json:"env_default"`
	PathPrepend   []string          `json:"path_prepend"`
	RequireEnv    []string          `json:"require_env"`
	NameArg       *NameArg          `json:"name_arg"`
	SessionLimits Limits            `json:"session_limits"`
}

// File is the whole configuration file.
type File struct {
	Accounts       []string           `json:"accounts"`
	ClaudePath     string             `json:"claude_path"`
	TmpDir         string             `json:"tmp_dir"`
	LinkDir        string             `json:"link_dir"`
	IgnoreProfiles []string           `json:"ignore_profiles"`
	ExtraProfiles  map[string]string  `json:"extra_profiles"`
	SessionLimits  Limits             `json:"session_limits"`
	Profiles       map[string]Profile `json:"profiles"`
	Configs        map[string]Config  `json:"configs"`
}

var (
	profileNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	configNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// ValidProfileName reports whether name may name a profile. "default" names
// ~/.claude; "tmp" is reserved because ~/.claude-tmp is the temp directory.
func ValidProfileName(name string) error {
	if !profileNameRE.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: use lower-case letters, digits and dashes, starting with a letter or digit", name)
	}
	if name == "tmp" {
		return fmt.Errorf("profile name %q is reserved", name)
	}
	return nil
}

// Path is where the configuration file lives.
func Path(env map[string]string, home string) string {
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return filepath.Join(x, "claude-profile", "config.json")
	}
	return filepath.Join(home, ".config", "claude-profile", "config.json")
}

// Load reads and validates the configuration file at path. A missing file
// gives the defaults.
func Load(path, home string) (*File, error) {
	var f File
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := f.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	f.finish(home)
	return &f, nil
}

func (f *File) validate() error {
	if err := checkLimits("session_limits", f.SessionLimits); err != nil {
		return err
	}
	for name, p := range f.Profiles {
		if err := ValidProfileName(name); err != nil {
			return fmt.Errorf("profiles: %w", err)
		}
		if err := checkLimits("profiles."+name+".slice_limits", p.SliceLimits); err != nil {
			return err
		}
		if err := checkLimits("profiles."+name+".session_limits", p.SessionLimits); err != nil {
			return err
		}
	}
	for name := range f.ExtraProfiles {
		if name == "default" {
			return fmt.Errorf("extra_profiles: %q is ~/.claude and cannot be redefined", name)
		}
		if err := ValidProfileName(name); err != nil {
			return fmt.Errorf("extra_profiles: %w", err)
		}
	}
	for name, c := range f.Configs {
		if !configNameRE.MatchString(name) || name == "claude-profile" {
			return fmt.Errorf("configs: invalid config name %q", name)
		}
		if c.Profile == "" {
			return fmt.Errorf("configs.%s: profile is required", name)
		}
		if err := ValidProfileName(c.Profile); err != nil {
			return fmt.Errorf("configs.%s: %w", name, err)
		}
		if c.NameArg != nil && c.NameArg.Env == "" {
			return fmt.Errorf("configs.%s: name_arg.env is required", name)
		}
		for _, m := range []map[string]string{c.Env, c.EnvDefault} {
			for k := range m {
				if k == "" || strings.Contains(k, "=") {
					return fmt.Errorf("configs.%s: invalid environment variable name %q", name, k)
				}
			}
		}
		if err := checkLimits("configs."+name+".session_limits", c.SessionLimits); err != nil {
			return err
		}
	}
	return nil
}

func checkLimits(where string, l Limits) error {
	for k, v := range l {
		if !slices.Contains(allowedLimits, k) {
			return fmt.Errorf("%s: %q is not one of %s", where, k, strings.Join(allowedLimits, ", "))
		}
		if v == "" || strings.ContainsAny(v, " \t\n") {
			return fmt.Errorf("%s: invalid value %q for %s", where, v, k)
		}
	}
	return nil
}

// finish fills in defaults and expands ~ in paths.
func (f *File) finish(home string) {
	if f.TmpDir == "" {
		f.TmpDir = DefaultTmpDir
	}
	if f.LinkDir == "" {
		f.LinkDir = DefaultLinkDir
	}
	f.TmpDir = Expand(home, f.TmpDir)
	f.LinkDir = Expand(home, f.LinkDir)
	f.ClaudePath = Expand(home, f.ClaudePath)
	merged := DefaultSessionLimits()
	maps.Copy(merged, f.SessionLimits)
	f.SessionLimits = merged
	for k, v := range f.ExtraProfiles {
		f.ExtraProfiles[k] = Expand(home, v)
	}
	for name, c := range f.Configs {
		c.PathPrepend = slices.Clone(c.PathPrepend)
		for i, p := range c.PathPrepend {
			c.PathPrepend[i] = Expand(home, p)
		}
		f.Configs[name] = c
	}
}

// Expand replaces a leading ~ or ~/ with home.
func Expand(home, p string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// SessionLimitsFor merges the session limits: top level, then the profile's,
// then the config's.
func (f *File) SessionLimitsFor(profileName, configName string) Limits {
	out := Limits{}
	maps.Copy(out, f.SessionLimits)
	maps.Copy(out, f.Profiles[profileName].SessionLimits)
	if configName != "" {
		maps.Copy(out, f.Configs[configName].SessionLimits)
	}
	return out
}

// ConfigNames lists the configs, sorted.
func (f *File) ConfigNames() []string { return slices.Sorted(maps.Keys(f.Configs)) }
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/config`.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Load and validate config.json

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 3: Profile discovery and account detection

**Files:**
- Create: `internal/profile/profile.go`, `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: `config.File`, `config.ValidProfileName`, `usage.Errorf`.
- Produces:
  - `type Profile struct{ Name, Dir string; Default bool; Account, Note, Expected string }`
  - `func (p Profile) GlobalJSON(home string) string` — `~/.claude.json` for default, `<Dir>/.claude.json` otherwise
  - `func (p Profile) Mismatch() bool`
  - `func Discover(home string, cfg *config.File) ([]Profile, error)` — default first, then by name
  - `func ReadAccount(path string) (account, note string)`
  - `func Find(ps []Profile, name string) (Profile, bool)`
  - `func ByDir(ps []Profile, dir string) (Profile, bool)` — compares symlink-resolved paths
  - `func Create(home, name string) (string, error)` — makes `~/.claude-<name>` mode 0700

- [ ] **Step 1: Write the failing test**

`internal/profile/profile_test.go`:

```go
package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func mkfile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func account(email string) string { return `{"oauthAccount": {"emailAddress": "` + email + `"}}` }

func TestDiscover(t *testing.T) {
	home := t.TempDir()
	// The default profile's account lives in ~/.claude.json, not ~/.claude/.claude.json.
	mkfile(t, filepath.Join(home, ".claude.json"), account("me@example.com"))
	mkfile(t, filepath.Join(home, ".claude", ".claude.json"), account("stale@example.com"))
	mkfile(t, filepath.Join(home, ".claude-work", ".claude.json"), account("work@example.com"))
	mkfile(t, filepath.Join(home, ".claude-fresh", ".credentials.json"), `{}`)
	mkfile(t, filepath.Join(home, ".claude-old", ".claude.json"), account("old@example.com"))
	mkfile(t, filepath.Join(home, ".claude-empty", "unrelated"), "")
	mkfile(t, filepath.Join(home, ".claude-Bad", ".claude.json"), account("x@example.com"))
	mkfile(t, filepath.Join(home, ".claude-tmp", ".claude.json"), account("x@example.com"))
	mkfile(t, filepath.Join(home, ".claude-broken", ".claude.json"), `{not json`)
	mkfile(t, filepath.Join(home, ".claude.json.backup"), "")
	extra := filepath.Join(home, "elsewhere")
	mkfile(t, filepath.Join(extra, ".claude.json"), account("extra@example.com"))
	cfg := &config.File{
		IgnoreProfiles: []string{"old"},
		ExtraProfiles:  map[string]string{"far": extra},
		Profiles:       map[string]config.Profile{"work": {Account: "BOSS@example.com"}},
	}

	got, err := Discover(home, cfg)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Name, Account string
		Default       bool
		Expected      string
		HasNote       bool
	}
	var rows []row
	for _, p := range got {
		rows = append(rows, row{p.Name, p.Account, p.Default, p.Expected, p.Note != ""})
	}
	want := []row{
		{"default", "me@example.com", true, "", false},
		{"broken", "", false, "", true},
		{"far", "extra@example.com", false, "", false},
		{"fresh", "", false, "", false},
		{"work", "work@example.com", false, "BOSS@example.com", false},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("Discover:\n got %+v\nwant %+v", rows, want)
	}

	work, _ := Find(got, "work")
	if !work.Mismatch() {
		t.Error("work.Mismatch() = false, want true")
	}
	if work.GlobalJSON(home) != filepath.Join(home, ".claude-work", ".claude.json") {
		t.Errorf("work.GlobalJSON = %q", work.GlobalJSON(home))
	}
	def, _ := Find(got, "default")
	if def.Mismatch() {
		t.Error("default.Mismatch() = true")
	}
	if def.GlobalJSON(home) != filepath.Join(home, ".claude.json") {
		t.Errorf("default.GlobalJSON = %q", def.GlobalJSON(home))
	}
	if _, ok := Find(got, "old"); ok {
		t.Error("ignored profile old was found")
	}
}

func TestMismatchIgnoresCase(t *testing.T) {
	p := Profile{Account: "A@Example.com", Expected: "a@example.COM"}
	if p.Mismatch() {
		t.Error("Mismatch() = true for the same address in another case")
	}
	if (Profile{Expected: "a@example.com"}).Mismatch() {
		t.Error("Mismatch() = true for a profile that is not logged in")
	}
}

func TestByDirFollowsSymlinks(t *testing.T) {
	home := t.TempDir()
	real := filepath.Join(home, "dot-claude")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	ps := []Profile{{Name: "default", Dir: filepath.Join(home, ".claude"), Default: true}}
	if p, ok := ByDir(ps, real); !ok || p.Name != "default" {
		t.Errorf("ByDir(real) = %+v, %v", p, ok)
	}
	if _, ok := ByDir(ps, filepath.Join(home, "other")); ok {
		t.Error("ByDir(other) found a profile")
	}
}

func TestCreate(t *testing.T) {
	home := t.TempDir()
	dir, err := Create(home, "new")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, ".claude-new") || fi.Mode().Perm() != 0o700 {
		t.Errorf("Create made %s with mode %v", dir, fi.Mode().Perm())
	}
	for _, name := range []string{"new", "default", "tmp", "Bad"} {
		if _, err := Create(home, name); !usage.Is(err) {
			t.Errorf("Create(%q) err = %v, want a usage error", name, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/profile` does not compile (`undefined: Discover`).

- [ ] **Step 3: Write the implementation**

`internal/profile/profile.go`:

```go
// Package profile finds the Claude Code config directories ("profiles") and
// the account each one is logged into.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/usage"
)

// Profile is one Claude Code config directory.
type Profile struct {
	Name     string
	Dir      string
	Default  bool   // ~/.claude, launched with CLAUDE_CONFIG_DIR unset
	Account  string // the logged-in email; "" when not logged in
	Note     string // why the account could not be read, if it could not
	Expected string // the account config.json expects; "" for none
}

// GlobalJSON is the .claude.json Claude Code reads for this profile. With
// CLAUDE_CONFIG_DIR unset (the default profile) that is ~/.claude.json;
// with it set, <dir>/.claude.json.
func (p Profile) GlobalJSON(home string) string {
	if p.Default {
		return filepath.Join(home, ".claude.json")
	}
	return filepath.Join(p.Dir, ".claude.json")
}

// Mismatch reports whether the profile is logged into a different account
// from the one config.json expects.
func (p Profile) Mismatch() bool {
	return p.Expected != "" && p.Account != "" && !strings.EqualFold(p.Expected, p.Account)
}

// Discover lists the profiles: ~/.claude as "default", every ~/.claude-<name>
// holding .claude.json or .credentials.json, and config.json's
// extra_profiles, minus its ignore_profiles. Default comes first, then the
// rest by name.
func Discover(home string, cfg *config.File) ([]Profile, error) {
	ignored := map[string]bool{}
	for _, n := range cfg.IgnoreProfiles {
		ignored[n] = true
	}
	found := map[string]Profile{}
	if def := filepath.Join(home, ".claude"); isDir(def) && !ignored["default"] {
		found["default"] = Profile{Name: "default", Dir: def, Default: true}
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude-*"))
	if err != nil {
		return nil, err
	}
	for _, dir := range matches {
		name := strings.TrimPrefix(filepath.Base(dir), ".claude-")
		if name == "default" || config.ValidProfileName(name) != nil || ignored[name] || !isDir(dir) {
			continue
		}
		if !exists(filepath.Join(dir, ".claude.json")) && !exists(filepath.Join(dir, ".credentials.json")) {
			continue
		}
		found[name] = Profile{Name: name, Dir: dir}
	}
	for name, dir := range cfg.ExtraProfiles {
		if !ignored[name] {
			found[name] = Profile{Name: name, Dir: dir}
		}
	}
	out := make([]Profile, 0, len(found))
	for _, p := range found {
		p.Account, p.Note = ReadAccount(p.GlobalJSON(home))
		p.Expected = cfg.Profiles[p.Name].Account
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ReadAccount returns oauthAccount.emailAddress from a .claude.json. A
// missing file or field means not logged in; note explains anything else.
func ReadAccount(path string) (account, note string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ""
	}
	if err != nil {
		return "", err.Error()
	}
	var g struct {
		OauthAccount *struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return "", fmt.Sprintf("%s: %v", path, err)
	}
	if g.OauthAccount == nil {
		return "", ""
	}
	return g.OauthAccount.EmailAddress, ""
}

// Find returns the profile with the given name.
func Find(ps []Profile, name string) (Profile, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// ByDir returns the profile whose directory is dir, after resolving symlinks.
func ByDir(ps []Profile, dir string) (Profile, bool) {
	want := realPath(dir)
	for _, p := range ps {
		if realPath(p.Dir) == want {
			return p, true
		}
	}
	return Profile{}, false
}

// Create makes the directory for a new profile, ~/.claude-<name>, mode 0700.
func Create(home, name string) (string, error) {
	if err := config.ValidProfileName(name); err != nil {
		return "", usage.Errorf("%v", err)
	}
	if name == "default" {
		return "", usage.Errorf("profile name %q is reserved", name)
	}
	dir := filepath.Join(home, ".claude-"+name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", usage.Errorf("%s already exists", dir)
		}
		return "", err
	}
	return dir, nil
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/profile`.

- [ ] **Step 5: Commit**

```bash
git add internal/profile
git commit -m "Discover profiles and the account each is logged into

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 4: Resume lookup

**Files:**
- Create: `internal/resume/resume.go`, `internal/resume/resume_test.go`

**Interfaces:**
- Consumes: `profile.Profile`.
- Produces:
  - `func Value(args []string) (string, bool)` — the session named by `--resume v`, `--resume=v` or `-r v`
  - `func IsUUID(s string) bool`
  - `type Hit struct{ Profile, SessionID, Transcript string }` (`Transcript` is the symlink-resolved path, `""` for a registry-only hit)
  - `func Lookup(profiles []profile.Profile, value string) ([]Hit, error)` — sorted by profile, then session ID; one hit per (profile, session)
  - `func Names(path string) (title, agent string)` — the last `custom-title` / `agent-name` in the transcript's last 256 KiB

- [ ] **Step 1: Write the failing test**

`internal/resume/resume_test.go`:

```go
package resume

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/profile"
)

const (
	idA = "11111111-2222-3333-4444-555555555555"
	idB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

func mkfile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func title(name, id string) string {
	return `{"type":"custom-title","customTitle":"` + name + `","sessionId":"` + id + `"}`
}

func agent(name, id string) string {
	return `{"type":"agent-name","agentName":"` + name + `","sessionId":"` + id + `"}`
}

func padding(n int) string { return `{"type":"user","message":"` + strings.Repeat("x", n) + `"}` }

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestValue(t *testing.T) {
	cases := []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "", false},
		{[]string{"--resume", "abc"}, "abc", true},
		{[]string{"-r", "abc"}, "abc", true},
		{[]string{"--resume=abc"}, "abc", true},
		{[]string{"--model", "opus", "--resume", "ac-issues", "hello"}, "ac-issues", true},
		{[]string{"--resume"}, "", false},
		{[]string{"--resume", "--model", "opus"}, "", false},
		{[]string{"--resume="}, "", false},
		{[]string{"--continue"}, "", false},
		{[]string{"--", "--resume", "abc"}, "", false},
	}
	for _, tc := range cases {
		got, ok := Value(tc.args)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Value(%q) = %q, %v; want %q, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !IsUUID(idA) || IsUUID("ac-issues") || IsUUID(strings.ToUpper(idB)) {
		t.Error("IsUUID gave a wrong answer")
	}
}

// The default profile's projects/ holds a symlink into the work profile's
// projects/, as adopt_projects.py leaves them: the hit belongs to work, once.
func TestLookupUUIDThroughBackSymlink(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".claude-work")
	def := filepath.Join(home, ".claude")
	mkfile(t, filepath.Join(work, "projects", "-src-a", idA+".jsonl"), "{}\n")
	if err := os.MkdirAll(filepath.Join(def, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "projects", "-src-a"), filepath.Join(def, "projects", "-src-a")); err != nil {
		t.Fatal(err)
	}
	ps := []profile.Profile{{Name: "default", Dir: def, Default: true}, {Name: "work", Dir: work}}

	got, err := Lookup(ps, idA)
	if err != nil {
		t.Fatal(err)
	}
	want := []Hit{{Profile: "work", SessionID: idA, Transcript: resolved(t, filepath.Join(work, "projects", "-src-a", idA+".jsonl"))}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lookup:\n got %+v\nwant %+v", got, want)
	}
}

func TestLookupUUIDInTwoProfiles(t *testing.T) {
	home := t.TempDir()
	ps := []profile.Profile{{Name: "a", Dir: filepath.Join(home, ".claude-a")}, {Name: "b", Dir: filepath.Join(home, ".claude-b")}}
	for _, p := range ps {
		mkfile(t, filepath.Join(p.Dir, "projects", "-x", idA+".jsonl"), "{}\n")
	}
	got, err := Lookup(ps, idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Profile != "a" || got[1].Profile != "b" {
		t.Errorf("Lookup = %+v, want hits in a and b", got)
	}
}

func TestLookupName(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".claude-work")
	def := filepath.Join(home, ".claude")
	ps := []profile.Profile{{Name: "default", Dir: def, Default: true}, {Name: "work", Dir: work}}
	// A running session, found through the registry.
	mkfile(t, filepath.Join(work, "sessions", "4242.json"), `{"pid":4242,"sessionId":"`+idA+`","name":"ac-issues"}`)
	// A renamed session: only its last name counts.
	renamed := filepath.Join(def, "projects", "-x", idB+".jsonl")
	mkfile(t, renamed, lines(title("old-name", idB), padding(100), title("new-name", idB), padding(100)))
	// A session with only an agent name, in a file larger than the tail.
	agented := filepath.Join(work, "projects", "-y", idA+".jsonl")
	mkfile(t, agented, lines(padding(300<<10), agent("bl-coordinator", idA)))

	cases := map[string][]Hit{
		"ac-issues":      {{Profile: "work", SessionID: idA}},
		"new-name":       {{Profile: "default", SessionID: idB, Transcript: resolved(t, renamed)}},
		"old-name":       nil,
		"bl-coordinator": {{Profile: "work", SessionID: idA, Transcript: resolved(t, agented)}},
		"nobody":         nil,
	}
	for name, want := range cases {
		got, err := Lookup(ps, name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Lookup(%q):\n got %+v\nwant %+v", name, got, want)
		}
	}
}

func TestNamesReadsOnlyTheTail(t *testing.T) {
	dir := t.TempDir()
	early := filepath.Join(dir, "early.jsonl")
	mkfile(t, early, lines(title("early", idA), padding(300<<10)))
	if got, _ := Names(early); got != "" {
		t.Errorf("Names(early) = %q, want \"\" (name is before the last 256 KiB)", got)
	}
	late := filepath.Join(dir, "late.jsonl")
	mkfile(t, late, lines(padding(300<<10), title("late", idA)))
	if got, _ := Names(late); got != "late" {
		t.Errorf("Names(late) = %q, want %q", got, "late")
	}
	if got, ag := Names(filepath.Join(dir, "missing.jsonl")); got != "" || ag != "" {
		t.Errorf("Names(missing) = %q, %q", got, ag)
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/resume` does not compile (`undefined: Value`).

- [ ] **Step 3: Write the implementation**

`internal/resume/resume.go`:

```go
// Package resume finds which profile owns the session a --resume argument
// names, by session UUID or by session name.
package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mithro/go-claude-profile/internal/profile"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID reports whether s looks like a Claude Code session ID.
func IsUUID(s string) bool { return uuidRE.MatchString(s) }

// Value returns the session named by --resume v, --resume=v or -r v. A
// --resume with no value (Claude's own picker) names no session.
func Value(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return "", false
		}
		if v, ok := strings.CutPrefix(a, "--resume="); ok {
			if v != "" {
				return v, true
			}
			continue
		}
		if a == "--resume" || a == "-r" {
			if i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1], true
			}
			return "", false
		}
	}
	return "", false
}

// Hit is one session that matches.
type Hit struct {
	Profile    string // the profile that owns the session
	SessionID  string
	Transcript string // symlink-resolved path; "" when found only in the registry
}

// Lookup finds the sessions value names in any profile. A transcript reached
// through a symlink (such as the back-symlinks under ~/.claude/projects)
// belongs to the profile whose directory really holds it, and counts once.
func Lookup(profiles []profile.Profile, value string) ([]Hit, error) {
	l := newLocator(profiles)
	if IsUUID(value) {
		for _, p := range profiles {
			matches, err := filepath.Glob(filepath.Join(p.Dir, "projects", "*", value+".jsonl"))
			if err != nil {
				return nil, err
			}
			for _, m := range matches {
				l.transcript(p.Name, m, value)
			}
		}
		return l.result(), nil
	}
	for _, p := range profiles {
		l.registry(p, value)
	}
	for _, p := range profiles {
		matches, err := filepath.Glob(filepath.Join(p.Dir, "projects", "*", "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			r, err := filepath.EvalSymlinks(m)
			if err != nil || l.seen[r] {
				continue
			}
			l.seen[r] = true
			if title, agent := Names(r); title == value || agent == value {
				id := strings.TrimSuffix(filepath.Base(r), ".jsonl")
				l.add(Hit{Profile: l.ownerOf(r, p.Name), SessionID: id, Transcript: r})
			}
		}
	}
	return l.result(), nil
}

// tailBytes is how much of each transcript Names reads. Claude Code repeats
// the name records with every turn, so the newest one is near the end.
const tailBytes = 256 << 10

// Names returns the last custom-title and agent-name recorded in the last
// 256 KiB of a transcript.
func Names(path string) (title, agent string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", ""
	}
	off := max(st.Size()-tailBytes, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return "", ""
	}
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		switch {
		case bytes.Contains(line, []byte(`"type":"custom-title"`)):
			var r struct {
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(line, &r) == nil && r.CustomTitle != "" {
				title = r.CustomTitle
			}
		case bytes.Contains(line, []byte(`"type":"agent-name"`)):
			var r struct {
				AgentName string `json:"agentName"`
			}
			if json.Unmarshal(line, &r) == nil && r.AgentName != "" {
				agent = r.AgentName
			}
		}
	}
	return title, agent
}

type owner struct{ name, dir string }

type locator struct {
	owners []owner          // symlink-resolved profile directories, longest first
	seen   map[string]bool  // resolved transcript paths already read
	hits   map[[2]string]Hit // keyed by profile and session ID
}

func newLocator(ps []profile.Profile) *locator {
	l := &locator{seen: map[string]bool{}, hits: map[[2]string]Hit{}}
	for _, p := range ps {
		if r, err := filepath.EvalSymlinks(p.Dir); err == nil {
			l.owners = append(l.owners, owner{p.Name, r})
		}
	}
	sort.Slice(l.owners, func(i, j int) bool { return len(l.owners[i].dir) > len(l.owners[j].dir) })
	return l
}

func (l *locator) ownerOf(realPath, fallback string) string {
	for _, o := range l.owners {
		if strings.HasPrefix(realPath, o.dir+string(filepath.Separator)) {
			return o.name
		}
	}
	return fallback
}

func (l *locator) transcript(profileName, path, id string) {
	r, err := filepath.EvalSymlinks(path)
	if err != nil || l.seen[r] {
		return
	}
	l.seen[r] = true
	l.add(Hit{Profile: l.ownerOf(r, profileName), SessionID: id, Transcript: r})
}

func (l *locator) registry(p profile.Profile, name string) {
	files, err := filepath.Glob(filepath.Join(p.Dir, "sessions", "*.json"))
	if err != nil {
		return
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e struct {
			SessionID string `json:"sessionId"`
			Name      string `json:"name"`
		}
		if json.Unmarshal(data, &e) == nil && e.Name == name && e.SessionID != "" {
			l.add(Hit{Profile: p.Name, SessionID: e.SessionID})
		}
	}
}

// add records h, preferring a hit that knows its transcript.
func (l *locator) add(h Hit) {
	key := [2]string{h.Profile, h.SessionID}
	if old, ok := l.hits[key]; ok && old.Transcript != "" {
		return
	}
	l.hits[key] = h
}

func (l *locator) result() []Hit {
	if len(l.hits) == 0 {
		return nil
	}
	out := make([]Hit, 0, len(l.hits))
	for _, h := range l.hits {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}
```

Note on `TestLookupName`'s `ac-issues` case: the registry hit has no transcript, and no transcript is named `ac-issues`, so `Transcript` stays `""`. In the `bl-coordinator` case the transcript has session ID `idA` in profile `work`, and the registry entry for `idA` is named `ac-issues`, not `bl-coordinator`, so only the transcript hit appears.

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/resume`.

- [ ] **Step 5: Commit**

```bash
git add internal/resume
git commit -m "Find the profile that owns a resumed session

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 5: Managed files and slice units

**Files:**
- Create: `internal/managed/managed.go`, `internal/managed/managed_test.go`, `internal/slice/slice.go`, `internal/slice/slice_test.go`

**Interfaces:**
- Consumes: `config.File`, `config.Limits`, `profile.Profile`.
- Produces:
  - `managed.Marker` (the exact string in Global Constraints)
  - `type managed.Result struct{ Written, Unchanged, Foreign []string }`, `func (r Result) Changed() bool`
  - `func managed.Sync(dir string, want map[string]string, dryRun bool) (Result, error)`
  - `func managed.Remove(dir, pattern string) ([]string, error)`
  - `func managed.Owned(data []byte) bool`
  - `slice.Top = "claude.slice"`, `func slice.Escape(s string) string`, `func slice.UnitName(profileName string) string`, `func slice.Render(description string, limits config.Limits) string`, `func slice.Desired(profiles []profile.Profile, cfg *config.File) map[string]string`, `func slice.UnitDir(env map[string]string, home string) string`, `func slice.InSlice(cgroup, profileName string) bool`

- [ ] **Step 1: Write the failing tests**

`internal/managed/managed_test.go`:

```go
package managed

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSync(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "units")
	owned := Marker + "\nnew\n"
	want := map[string]string{"a": owned, "b": owned, "c": owned, "d": owned}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b"), []byte(owned), 0o644)                  // already right
	os.WriteFile(filepath.Join(dir, "c"), []byte(Marker+"\nold\n"), 0o644)      // ours, out of date
	os.WriteFile(filepath.Join(dir, "d"), []byte("# the user's own\n"), 0o644) // not ours

	dry, err := Sync(dir, want, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); err == nil {
		t.Error("dry run wrote a file")
	}
	res, err := Sync(dir, want, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dry, res) {
		t.Errorf("dry run %+v differs from real run %+v", dry, res)
	}
	j := func(n string) string { return filepath.Join(dir, n) }
	wantRes := Result{Written: []string{j("a"), j("c")}, Unchanged: []string{j("b")}, Foreign: []string{j("d")}}
	if !reflect.DeepEqual(res, wantRes) {
		t.Errorf("Sync = %+v, want %+v", res, wantRes)
	}
	if !res.Changed() {
		t.Error("Changed() = false")
	}
	if read(t, j("a")) != owned || read(t, j("c")) != owned || read(t, j("d")) != "# the user's own\n" {
		t.Error("file contents wrong after Sync")
	}
	again, err := Sync(dir, want, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed() {
		t.Errorf("second Sync wrote %v", again.Written)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude.slice"), []byte(Marker+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "claude-x.slice"), []byte("mine\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "other.slice"), []byte(Marker+"\n"), 0o644)
	got, err := Remove(dir, "claude*.slice")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{filepath.Join(dir, "claude.slice")}) {
		t.Errorf("Remove = %q", got)
	}
	for _, n := range []string{"claude-x.slice", "other.slice"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s was removed", n)
		}
	}
}
```

`internal/slice/slice_test.go`:

```go
package slice

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/profile"
)

func TestUnitName(t *testing.T) {
	for in, want := range map[string]string{
		"default":     "claude-default.slice",
		"west-corp": `claude-west\x2dcorp.slice`,
		"fo2":         "claude-fo2.slice",
	} {
		if got := UnitName(in); got != want {
			t.Errorf("UnitName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRender(t *testing.T) {
	got := Render("Claude Code profile work", config.Limits{"MemoryMax": "24G", "CPUQuota": "400%"})
	want := managed.Marker + "\n[Unit]\nDescription=Claude Code profile work\n\n[Slice]\nCPUQuota=400%\nMemoryMax=24G\n"
	if got != want {
		t.Errorf("Render:\n%s\nwant:\n%s", got, want)
	}
}

func TestDesired(t *testing.T) {
	ps := []profile.Profile{{Name: "default"}, {Name: "west-corp"}}
	cfg := &config.File{Profiles: map[string]config.Profile{"west-corp": {SliceLimits: config.Limits{"MemoryMax": "24G"}}}}
	got := Desired(ps, cfg)
	keys := slices.Sorted(maps.Keys(got))
	if want := []string{"claude-default.slice", `claude-west\x2dcorp.slice`, "claude.slice"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("Desired keys = %q, want %q", keys, want)
	}
	if got[`claude-west\x2dcorp.slice`] != Render("Claude Code profile west-corp", config.Limits{"MemoryMax": "24G"}) {
		t.Errorf("west-corp unit = %q", got[`claude-west\x2dcorp.slice`])
	}
}

func TestInSlice(t *testing.T) {
	in := "0::/user.slice/user-1000.slice/user@1000.service/claude.slice/claude-west\\x2dcorp.slice/run-p1-i2.scope\n"
	if !InSlice(in, "west-corp") {
		t.Error("InSlice(scope in west-corp) = false")
	}
	if InSlice(in, "west") || InSlice(in, "default") {
		t.Error("InSlice matched another profile")
	}
	if InSlice("0::/user.slice/user-1000.slice/user@1000.service/app.slice/x.scope\n", "default") {
		t.Error("InSlice matched app.slice")
	}
	if !InSlice("0::/user.slice/user-1000.slice/user@1000.service/claude.slice/claude-default.slice\n", "default") {
		t.Error("InSlice(the slice itself) = false")
	}
}

func TestUnitDir(t *testing.T) {
	if got := UnitDir(map[string]string{}, "/h"); got != "/h/.config/systemd/user" {
		t.Errorf("UnitDir = %q", got)
	}
	if got := UnitDir(map[string]string{"XDG_CONFIG_HOME": "/x"}, "/h"); got != "/x/systemd/user" {
		t.Errorf("UnitDir(XDG) = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `make test`
Expected: FAIL — `internal/managed` and `internal/slice` do not compile.

- [ ] **Step 3: Write the implementation**

`internal/managed/managed.go`:

```go
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
```

`internal/slice/slice.go`:

```go
// Package slice names and renders the systemd user slices that hold Claude
// sessions: claude.slice, one claude-<profile>.slice per profile beneath it,
// and one scope per session beneath that.
package slice

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/profile"
)

// Top is the slice every Claude session sits under.
const Top = "claude.slice"

// Escape escapes s as `systemd-escape` does, so a dash in a profile name
// does not add a level to the slice hierarchy.
func Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ':', c == '_', c == '.' && i > 0:
			b.WriteByte(c)
		case c == '/':
			b.WriteByte('-')
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// UnitName is the slice for a profile, a child of claude.slice.
func UnitName(profileName string) string { return "claude-" + Escape(profileName) + ".slice" }

// Render returns a slice unit file.
func Render(description string, limits config.Limits) string {
	var b strings.Builder
	b.WriteString(managed.Marker + "\n[Unit]\nDescription=" + description + "\n\n[Slice]\n")
	for _, k := range slices.Sorted(maps.Keys(limits)) {
		fmt.Fprintf(&b, "%s=%s\n", k, limits[k])
	}
	return b.String()
}

// Desired returns every slice unit file, by file name.
func Desired(profiles []profile.Profile, cfg *config.File) map[string]string {
	want := map[string]string{Top: Render("Claude Code sessions (claude-profile)", nil)}
	for _, p := range profiles {
		want[UnitName(p.Name)] = Render("Claude Code profile "+p.Name, cfg.Profiles[p.Name].SliceLimits)
	}
	return want
}

// UnitDir is where systemd reads user unit files.
func UnitDir(env map[string]string, home string) string {
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return filepath.Join(x, "systemd", "user")
	}
	return filepath.Join(home, ".config", "systemd", "user")
}

// InSlice reports whether the process whose /proc/<pid>/cgroup is cgroup
// already runs inside the profile's slice.
func InSlice(cgroup, profileName string) bool {
	want := "/" + Top + "/" + UnitName(profileName) + "/"
	for _, line := range strings.Split(cgroup, "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok && strings.Contains(p+"/", want) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok` for `internal/managed` and `internal/slice`.

- [ ] **Step 5: Commit**

```bash
git add internal/managed internal/slice
git commit -m "Render and sync the per-profile slice units

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 6: Temp directory checks

**Files:**
- Create: `internal/tmpdir/tmpdir.go`, `internal/tmpdir/tmpdir_test.go`

**Interfaces:**
- Produces: `tmpdir.MaxLen = 44`, `func tmpdir.Check(dir string, uid int) error`, `func tmpdir.Ensure(dir string) error`

- [ ] **Step 1: Write the failing test**

`internal/tmpdir/tmpdir_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/tmpdir` does not compile.

- [ ] **Step 3: Write the implementation**

`internal/tmpdir/tmpdir.go`:

```go
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
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/tmpdir`.

- [ ] **Step 5: Commit**

```bash
git add internal/tmpdir
git commit -m "Check that Claude's temp directory is on disk

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 7: Building the launch command

**Files:**
- Create: `internal/launch/launch.go`, `internal/launch/launch_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Limits`, `profile.Profile`, `slice.UnitName`, `slice.InSlice`, `usage.Errorf`.
- Produces:
  - `launch.NoCgroupEnv = "CLAUDE_PROFILE_NO_CGROUP"`
  - `type Request struct{ Env map[string]string; Profile profile.Profile; ConfigName string; Config *config.Config; Args []string; Claude, SystemdRun, TmpDir string; Limits config.Limits }`
  - `type Plan struct{ Argv, Env, Warnings []string }`
  - `func Build(r Request) (Plan, error)`
  - `func WantScope(env map[string]string, cgroup, profileName string) bool`
  - `func ResolveClaude(configured, pathEnv, self string) (string, error)`
  - `func EnvList(m map[string]string) []string` (sorted `K=V`)

- [ ] **Step 1: Write the failing test**

`internal/launch/launch_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/launch` does not compile.

- [ ] **Step 3: Write the implementation**

`internal/launch/launch.go`:

```go
// Package launch turns a chosen profile and config into the exact command and
// environment that start Claude Code.
package launch

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/usage"
)

// NoCgroupEnv, set to 1, runs Claude without a new scope.
const NoCgroupEnv = "CLAUDE_PROFILE_NO_CGROUP"

// Request is everything a launch depends on.
type Request struct {
	Env        map[string]string // the caller's environment
	Profile    profile.Profile
	ConfigName string         // "" without a config
	Config     *config.Config // nil without a config
	Args       []string       // the user's arguments for Claude
	Claude     string         // the real claude binary
	SystemdRun string         // systemd-run's path; "" runs Claude directly
	TmpDir     string
	Limits     config.Limits // the session scope's limits
}

// Plan is the command to exec.
type Plan struct {
	Argv     []string
	Env      []string
	Warnings []string
}

// WantScope reports whether a launch should start a new scope: not when the
// caller opted out, nor when it already runs inside the profile's slice.
func WantScope(env map[string]string, cgroup, profileName string) bool {
	return env[NoCgroupEnv] != "1" && !slice.InSlice(cgroup, profileName)
}

// Build computes the command and environment for r.
func Build(r Request) (Plan, error) {
	env := maps.Clone(r.Env)
	if env == nil {
		env = map[string]string{}
	}
	c := r.Config
	if c == nil {
		c = &config.Config{}
	}
	label := r.ConfigName
	if label == "" {
		label = r.Profile.Name
	}
	args := slices.Clone(r.Args)
	var nameArgs []string
	if c.NameArg != nil && env[c.NameArg.Env] == "" {
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return Plan{}, usage.Errorf("%s needs a NAME as its first argument (or set %s)", label, c.NameArg.Env)
		}
		name := c.NameArg.Prefix + args[0]
		args = args[1:]
		env[c.NameArg.Env] = name
		nameArgs = []string{"--name", name}
	}
	for _, k := range c.RequireEnv {
		if env[k] == "" {
			return Plan{}, usage.Errorf("%s needs %s to be set", label, k)
		}
	}

	if r.Profile.Default {
		delete(env, "CLAUDE_CONFIG_DIR")
	} else {
		env["CLAUDE_CONFIG_DIR"] = r.Profile.Dir
	}
	env["CLAUDE_PROFILE"] = r.Profile.Name
	env["CLAUDE_CODE_TMPDIR"] = r.TmpDir
	env["TMPDIR"] = r.TmpDir
	maps.Copy(env, c.Env)
	for k, v := range c.EnvDefault {
		if env[k] == "" {
			env[k] = v
		}
	}
	if len(c.PathPrepend) > 0 {
		parts := slices.Clone(c.PathPrepend)
		if p := env["PATH"]; p != "" {
			parts = append(parts, p)
		}
		env["PATH"] = strings.Join(parts, ":")
	}

	var plan Plan
	if r.Profile.Mismatch() {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("profile %s is logged in as %s, but config.json expects %s",
			r.Profile.Name, r.Profile.Account, r.Profile.Expected))
	}
	claudeArgv := append([]string{r.Claude}, c.Args...)
	claudeArgv = append(claudeArgv, nameArgs...)
	claudeArgv = append(claudeArgv, args...)
	if r.SystemdRun == "" {
		if r.Env[NoCgroupEnv] == "1" {
			plan.Warnings = append(plan.Warnings, NoCgroupEnv+"=1: Claude runs without resource limits")
		}
		plan.Argv = claudeArgv
	} else {
		desc := "Claude " + r.Profile.Name
		if r.ConfigName != "" {
			desc += " [" + r.ConfigName + "]"
		}
		argv := []string{r.SystemdRun, "--user", "--scope", "--collect", "--quiet", "--same-dir",
			"--slice=" + slice.UnitName(r.Profile.Name), "--description=" + desc}
		for _, k := range slices.Sorted(maps.Keys(r.Limits)) {
			argv = append(argv, "-p", k+"="+r.Limits[k])
		}
		argv = append(argv, "--")
		plan.Argv = append(argv, claudeArgv...)
	}
	plan.Env = EnvList(env)
	return plan, nil
}

// ResolveClaude finds the real claude binary: configured if set, otherwise
// the first claude on pathEnv that is not this program (self, resolved).
func ResolveClaude(configured, pathEnv, self string) (string, error) {
	if configured != "" {
		if !isExecutable(configured) {
			return "", usage.Errorf("claude_path %s is not an executable file", configured)
		}
		return configured, nil
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, "claude")
		if !isExecutable(cand) {
			continue
		}
		if r, err := filepath.EvalSymlinks(cand); err == nil && r == self {
			continue
		}
		return cand, nil
	}
	return "", errors.New("no claude on PATH other than claude-profile itself (set claude_path in config.json)")
}

// EnvList turns an environment map into sorted K=V strings.
func EnvList(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, k+"="+m[k])
	}
	return out
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/launch`.

- [ ] **Step 5: Commit**

```bash
git add internal/launch
git commit -m "Build the systemd-run command that starts Claude

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 8: The interactive menu

**Files:**
- Create: `internal/menu/menu.go`, `internal/menu/menu_test.go`

**Interfaces:**
- Consumes: `config.File` (`Accounts`, `Configs`, `ConfigNames`), `profile.Profile` (`Mismatch`).
- Produces:
  - `type Kind int` with `KindProfile`, `KindConfig`, `KindNew`
  - `type Item struct{ Kind Kind; Profile, Config, Account string }`, `func (i Item) Key() string`
  - `type Entry struct{ Label string; Item Item }`, `type Group struct{ Heading string; Entries []Entry }`
  - `var ErrCancelled`
  - `func Build(profiles []profile.Profile, cfg *config.File) []Group`
  - `func Choose(groups []Group, br *bufio.Reader, w io.Writer, note, def string) (Item, error)`
  - `func Prompt(br *bufio.Reader, w io.Writer, question string) (string, error)`
  - `func StatePath(env map[string]string, home string) string`, `func LoadLast(path string) string`, `func SaveLast(path, key string) error`

- [ ] **Step 1: Write the failing test**

`internal/menu/menu_test.go`:

```go
package menu

import (
	"bufio"
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
)

func fixture() ([]profile.Profile, *config.File) {
	ps := []profile.Profile{
		{Name: "default", Dir: "/h/.claude", Default: true, Account: "me@example.com"},
		{Name: "fresh", Dir: "/h/.claude-fresh"},
		{Name: "west-corp", Dir: "/h/.claude-west-corp", Account: "c@example.com", Expected: "w@example.com"},
	}
	cfg := &config.File{
		Accounts: []string{"w@example.com", "ME@example.com"},
		Configs: map[string]config.Config{
			"claude-ac":             {Profile: "west-corp"},
			"claude-ac-coordinator": {Profile: "west-corp"},
			"claude-gone":           {Profile: "missing"},
		},
	}
	return ps, cfg
}

func TestBuild(t *testing.T) {
	groups := Build(fixture())
	var headings []string
	var keys []string
	for _, g := range groups {
		headings = append(headings, g.Heading)
		for _, e := range g.Entries {
			keys = append(keys, e.Item.Key())
		}
	}
	wantHeadings := []string{"c@example.com", "me@example.com", "w@example.com  (no profile yet)", "not logged in"}
	if !reflect.DeepEqual(headings, wantHeadings) {
		t.Errorf("headings = %q, want %q", headings, wantHeadings)
	}
	wantKeys := []string{
		"profile:west-corp", "config:claude-ac", "config:claude-ac-coordinator", "new:c@example.com",
		"profile:default", "new:me@example.com",
		"new:w@example.com",
		"profile:fresh", "new:",
	}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Errorf("keys:\n got %q\nwant %q", keys, wantKeys)
	}
	if label := groups[0].Entries[0].Label; !strings.Contains(label, "!! expected w@example.com") {
		t.Errorf("mismatch label = %q", label)
	}
}

func TestChoose(t *testing.T) {
	groups := Build(fixture())
	cases := []struct {
		input, def string
		want       string // item key, or "cancel"
	}{
		{"2\n", "", "config:claude-ac"},
		{"\n", "profile:default", "profile:default"},
		{"x\n0\n99\n5\n", "", "profile:default"},
		{"q\n", "", "cancel"},
		{"", "", "cancel"},
		{"\n", "", "cancel"},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		item, err := Choose(groups, bufio.NewReader(strings.NewReader(tc.input)), &out, "Session x exists in several profiles.", tc.def)
		got := item.Key()
		if errors.Is(err, ErrCancelled) {
			got = "cancel"
		} else if err != nil {
			t.Fatalf("input %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("input %q def %q: got %s, want %s\n%s", tc.input, tc.def, got, tc.want, out.String())
		}
		if !strings.HasPrefix(out.String(), "Session x exists in several profiles.\n") {
			t.Errorf("note not printed first: %q", out.String())
		}
	}
}

func TestChooseShowsDefault(t *testing.T) {
	var out bytes.Buffer
	Choose(Build(fixture()), bufio.NewReader(strings.NewReader("q\n")), &out, "", "profile:default")
	if !strings.Contains(out.String(), "  5) default") || !strings.Contains(out.String(), "[5]") {
		t.Errorf("menu = %q", out.String())
	}
}

func TestPrompt(t *testing.T) {
	var out bytes.Buffer
	got, err := Prompt(bufio.NewReader(strings.NewReader("  newname \n")), &out, "Name: ")
	if err != nil || got != "newname" || out.String() != "Name: " {
		t.Errorf("Prompt = %q, %v (printed %q)", got, err, out.String())
	}
	if _, err := Prompt(bufio.NewReader(strings.NewReader("\n")), &out, "Name: "); !errors.Is(err, ErrCancelled) {
		t.Errorf("empty answer: err = %v", err)
	}
}

func TestLast(t *testing.T) {
	home := t.TempDir()
	path := StatePath(map[string]string{}, home)
	if path != filepath.Join(home, ".local", "state", "claude-profile", "last.json") {
		t.Errorf("StatePath = %q", path)
	}
	if got := LoadLast(path); got != "" {
		t.Errorf("LoadLast before save = %q", got)
	}
	if err := SaveLast(path, "config:claude-ac"); err != nil {
		t.Fatal(err)
	}
	if got := LoadLast(path); got != "config:claude-ac" {
		t.Errorf("LoadLast = %q", got)
	}
	if got := StatePath(map[string]string{"XDG_STATE_HOME": "/s"}, home); got != "/s/claude-profile/last.json" {
		t.Errorf("StatePath(XDG) = %q", got)
	}
}
```

Numbering in this fixture: 1 west-corp, 2 claude-ac, 3 claude-ac-coordinator, 4 new for c@, 5 default, 6 new for me@, 7 new for w@, 8 fresh, 9 new.

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — `internal/menu` does not compile.

- [ ] **Step 3: Write the implementation**

`internal/menu/menu.go`:

```go
// Package menu is the interactive profile chooser: a numbered list on the
// terminal, grouped by account.
package menu

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
)

// Kind is what choosing an item does.
type Kind int

const (
	KindProfile Kind = iota // launch in a profile
	KindConfig              // launch with a config
	KindNew                 // create a profile, then launch in it
)

// Item is one choice.
type Item struct {
	Kind    Kind
	Profile string // KindProfile, KindConfig
	Config  string // KindConfig
	Account string // KindNew: the account to log in as ("" for any)
}

// Key identifies an item across runs, for remembering the last choice.
func (i Item) Key() string {
	switch i.Kind {
	case KindProfile:
		return "profile:" + i.Profile
	case KindConfig:
		return "config:" + i.Config
	default:
		return "new:" + i.Account
	}
}

// Entry is an item with its label.
type Entry struct {
	Label string
	Item  Item
}

// Group is the entries for one account.
type Group struct {
	Heading string
	Entries []Entry
}

// ErrCancelled is returned when the user quits the menu.
var ErrCancelled = errors.New("cancelled")

// Build groups the profiles by account: logged-in accounts (sorted), then
// config.json's pinned accounts that have no profile, then "not logged in".
func Build(profiles []profile.Profile, cfg *config.File) []Group {
	byAccount := map[string][]profile.Profile{}
	display := map[string]string{}
	var loggedOut []profile.Profile
	for _, p := range profiles {
		if p.Account == "" {
			loggedOut = append(loggedOut, p)
			continue
		}
		key := strings.ToLower(p.Account)
		byAccount[key] = append(byAccount[key], p)
		if _, ok := display[key]; !ok {
			display[key] = p.Account
		}
	}
	var groups []Group
	for _, key := range slices.Sorted(maps.Keys(byAccount)) {
		g := Group{Heading: display[key]}
		for _, p := range byAccount[key] {
			g.Entries = append(g.Entries, profileEntries(p, cfg)...)
		}
		g.Entries = append(g.Entries, newEntry(display[key]))
		groups = append(groups, g)
	}
	pinned := map[string]bool{}
	for _, acct := range cfg.Accounts {
		key := strings.ToLower(acct)
		if _, ok := byAccount[key]; ok || pinned[key] {
			continue
		}
		pinned[key] = true
		groups = append(groups, Group{Heading: acct + "  (no profile yet)", Entries: []Entry{newEntry(acct)}})
	}
	g := Group{Heading: "not logged in"}
	for _, p := range loggedOut {
		g.Entries = append(g.Entries, profileEntries(p, cfg)...)
	}
	g.Entries = append(g.Entries, newEntry(""))
	return append(groups, g)
}

func profileEntries(p profile.Profile, cfg *config.File) []Entry {
	label := fmt.Sprintf("%-16s %s", p.Name, p.Dir)
	if p.Mismatch() {
		label += "  !! expected " + p.Expected
	}
	if p.Note != "" {
		label += "  (" + p.Note + ")"
	}
	entries := []Entry{{Label: label, Item: Item{Kind: KindProfile, Profile: p.Name}}}
	for _, name := range cfg.ConfigNames() {
		if cfg.Configs[name].Profile == p.Name {
			entries = append(entries, Entry{Label: "  " + name + "  (config)", Item: Item{Kind: KindConfig, Profile: p.Name, Config: name}})
		}
	}
	return entries
}

func newEntry(account string) Entry {
	label := "+ new profile"
	if account != "" {
		label += " for " + account
	}
	return Entry{Label: label, Item: Item{Kind: KindNew, Account: account}}
}

// Choose prints the menu to w and reads a choice from br. def is the key of
// the item Enter chooses. note, if any, is printed above the menu.
func Choose(groups []Group, br *bufio.Reader, w io.Writer, note, def string) (Item, error) {
	if note != "" {
		fmt.Fprintln(w, note)
	}
	var items []Item
	defNum := 0
	for _, g := range groups {
		fmt.Fprintln(w, g.Heading)
		for _, e := range g.Entries {
			items = append(items, e.Item)
			if e.Item.Key() == def {
				defNum = len(items)
			}
			fmt.Fprintf(w, "  %3d) %s\n", len(items), e.Label)
		}
	}
	for {
		if defNum > 0 {
			fmt.Fprintf(w, "Choose 1-%d [%d], q to quit: ", len(items), defNum)
		} else {
			fmt.Fprintf(w, "Choose 1-%d, q to quit: ", len(items))
		}
		line, err := br.ReadString('\n')
		ans := strings.TrimSpace(line)
		switch {
		case ans == "q":
			return Item{}, ErrCancelled
		case ans == "" && defNum > 0:
			return items[defNum-1], nil
		case ans == "":
			return Item{}, ErrCancelled
		}
		if n, convErr := strconv.Atoi(ans); convErr == nil && n >= 1 && n <= len(items) {
			return items[n-1], nil
		}
		fmt.Fprintf(w, "%q is not a choice.\n", ans)
		if err != nil {
			return Item{}, ErrCancelled
		}
	}
}

// Prompt asks a question and returns the trimmed answer; an empty answer
// cancels.
func Prompt(br *bufio.Reader, w io.Writer, question string) (string, error) {
	fmt.Fprint(w, question)
	line, _ := br.ReadString('\n')
	ans := strings.TrimSpace(line)
	if ans == "" {
		return "", ErrCancelled
	}
	return ans, nil
}

// StatePath is where the last choice is remembered.
func StatePath(env map[string]string, home string) string {
	base := env["XDG_STATE_HOME"]
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claude-profile", "last.json")
}

type lastFile struct {
	Last string `json:"last"`
}

// LoadLast returns the remembered choice's key, or "".
func LoadLast(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var l lastFile
	if json.Unmarshal(data, &l) != nil {
		return ""
	}
	return l.Last
}

// SaveLast remembers a choice's key.
func SaveLast(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(lastFile{Last: key})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
```

In `TestChoose`, the case `"\n"` with no default cancels (an empty answer with nothing to default to); the case `"x\n0\n99\n5\n"` rejects three bad answers, then picks 5 (`default`).

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: `ok  github.com/mithro/go-claude-profile/internal/menu`.

- [ ] **Step 5: Commit**

```bash
git add internal/menu
git commit -m "Add the account-grouped profile menu

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 9: Full dependencies, `list` and `which`

**Files:**
- Modify (replace): `internal/cli/cli.go`, `internal/cli/helpers_test.go`, `internal/cli/cli_test.go`
- Create: `internal/cli/list.go`, `internal/cli/which.go`, `internal/cli/list_test.go`, `internal/cli/which_test.go`

**Interfaces:**
- Consumes: `config.Load`, `config.Path`, `profile.Discover`, `profile.Find`, `profile.ByDir`, `resume.Lookup`, `menu.Build`, `menu.KindNew`, `tmpdir.Check`, `usage`.
- Produces (used by Tasks 10–11):
  - `Deps` fields: `Env map[string]string; UID int; Stdin io.Reader; Stdout, Stderr io.Writer; Self, ProcRoot, CgroupRoot string; Exec func(path string, argv, env []string) error; Run func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error; LookPath func(string) (string, error); OpenTTY func() (io.ReadCloser, error); CheckTmp func(dir string, uid int) error`
  - `type world struct{ home string; cfg *config.File; profiles []profile.Profile }`, `func load(d Deps) (*world, error)`
  - `func parseFlags(fs *flag.FlagSet, args []string) (int, bool)`, `func writeJSON(d Deps, v any) int`, `func profileNames(ps []profile.Profile) []string`
  - test fixture `newFixture(t)` with methods `profile(name, email)`, `config(body)`, `run(argv0, args...)`, `write(path, body, mode)`

- [ ] **Step 1: Replace the test helpers and write the failing tests**

`internal/cli/helpers_test.go` (replaces the Task 1 file):

```go
package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type execCall struct {
	path      string
	argv, env []string
}

type fixture struct {
	t     *testing.T
	root  string
	home  string
	bin   string // on PATH; holds a fake claude
	d     Deps
	out   *bytes.Buffer
	errb  *bytes.Buffer
	execs []execCall
	runs  [][]string
	tty   string // menu answers; "" means there is no terminal
}

var errExecRecorded = errors.New("exec recorded")

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	f.write(filepath.Join(f.bin, "claude"), "#!/bin/sh\nexit 0\n", 0o755)
	f.write(filepath.Join(root, "claude-profile"), "#!/bin/sh\nexit 0\n", 0o755)
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	f.d = Deps{
		Env: map[string]string{
			"HOME":            f.home,
			"PATH":            f.bin,
			"XDG_CONFIG_HOME": filepath.Join(f.home, ".config"),
			"XDG_STATE_HOME":  filepath.Join(f.home, ".local", "state"),
		},
		UID:        1000,
		Stdin:      strings.NewReader(""),
		Stdout:     f.out,
		Stderr:     f.errb,
		Self:       filepath.Join(root, "claude-profile"),
		ProcRoot:   filepath.Join(root, "proc"),
		CgroupRoot: filepath.Join(root, "cgroup"),
		Exec: func(path string, argv, env []string) error {
			f.execs = append(f.execs, execCall{path, argv, env})
			return errExecRecorded
		},
		Run: func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
			f.runs = append(f.runs, argv)
			return nil
		},
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		OpenTTY: func() (io.ReadCloser, error) {
			if f.tty == "" {
				return nil, errors.New("no terminal")
			}
			return io.NopCloser(strings.NewReader(f.tty)), nil
		},
		CheckTmp: func(string, int) error { return nil },
	}
	return f
}

func (f *fixture) write(path, body string, mode fs.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		f.t.Fatal(err)
	}
}

// profile creates a logged-in profile ("" email: logged out).
func (f *fixture) profile(name, email string) string {
	f.t.Helper()
	body := `{}`
	if email != "" {
		body = `{"oauthAccount": {"emailAddress": "` + email + `"}}`
	}
	if name == "default" {
		dir := filepath.Join(f.home, ".claude")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			f.t.Fatal(err)
		}
		f.write(filepath.Join(f.home, ".claude.json"), body, 0o644)
		return dir
	}
	dir := filepath.Join(f.home, ".claude-"+name)
	f.write(filepath.Join(dir, ".claude.json"), body, 0o644)
	return dir
}

func (f *fixture) config(body string) {
	f.t.Helper()
	f.write(filepath.Join(f.home, ".config", "claude-profile", "config.json"), body, 0o644)
}

func (f *fixture) run(argv0 string, args ...string) int {
	f.t.Helper()
	f.out.Reset()
	f.errb.Reset()
	return RunMultiCall(argv0, args, f.d)
}
```

`internal/cli/cli_test.go` (replaces the Task 1 file):

```go
package cli

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	f := newFixture(t)
	for _, arg := range []string{"version", "--version", "-v"} {
		if got := f.run("/usr/bin/claude-profile", arg); got != exitOK {
			t.Fatalf("%s: exit %d", arg, got)
		}
		if got, want := f.out.String(), "claude-profile dev\n"; got != want {
			t.Errorf("%s: got %q, want %q", arg, got, want)
		}
	}
}

func TestNoArgumentsIsUsage(t *testing.T) {
	f := newFixture(t)
	if got := f.run("claude-profile"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(f.errb.String(), "usage: claude-profile") {
		t.Errorf("stderr = %q", f.errb.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	f := newFixture(t)
	if got := f.run("claude-profile", "frob"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if want := `claude-profile: unknown command "frob"`; !strings.Contains(f.errb.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", f.errb.String(), want)
	}
}

func TestBrokenConfigStopsCommands(t *testing.T) {
	f := newFixture(t)
	f.config(`{"nonsense": 1}`)
	if got := f.run("claude-profile", "list"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(f.errb.String(), `unknown field "nonsense"`) {
		t.Errorf("stderr = %q", f.errb.String())
	}
	if got := f.run("claude-profile", "version"); got != exitOK {
		t.Errorf("version with a broken config: exit %d", got)
	}
}
```

`internal/cli/list_test.go`:

```go
package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestListJSON(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	f.profile("fresh", "")
	f.config(`{"accounts": ["w@example.com"], "profiles": {"work": {"account": "w@example.com"}},
		"configs": {"claude-w": {"profile": "work"}}}`)
	if got := f.run("claude-profile", "list", "--json"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var out listOutput
	if err := json.Unmarshal(f.out.Bytes(), &out); err != nil {
		t.Fatalf("%v in %s", err, f.out)
	}
	if want := []string{"c@example.com", "me@example.com", "w@example.com"}; !reflect.DeepEqual(out.Accounts, want) {
		t.Errorf("accounts = %q, want %q", out.Accounts, want)
	}
	var names []string
	for _, p := range out.Profiles {
		names = append(names, p.Name)
	}
	if want := []string{"default", "fresh", "work"}; !reflect.DeepEqual(names, want) {
		t.Errorf("profiles = %q, want %q", names, want)
	}
	work := out.Profiles[2]
	if work.Account != "c@example.com" || work.ExpectedAccount != "w@example.com" || !work.LoggedIn || work.Dir != f.home+"/.claude-work" {
		t.Errorf("work = %+v", work)
	}
	if out.Profiles[1].LoggedIn {
		t.Error("fresh is logged in")
	}
	if want := []configOut{{Name: "claude-w", Profile: "work"}}; !reflect.DeepEqual(out.Configs, want) {
		t.Errorf("configs = %+v", out.Configs)
	}
}

func TestListText(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	if got := f.run("claude-profile", "list"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if !strings.Contains(f.out.String(), "me@example.com\n  default ") || strings.Contains(f.out.String(), "new profile") {
		t.Errorf("list = %q", f.out.String())
	}
}
```

`internal/cli/which_test.go`:

```go
package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const sid = "11111111-2222-3333-4444-555555555555"

func TestWhichSession(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(work, "projects", "-src", sid+".jsonl"), "{}\n", 0o644)
	if got := f.run("claude-profile", "which", "--json", sid); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var rows []whichRow
	if err := json.Unmarshal(f.out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Profile != "work" || rows[0].Account != "c@example.com" || rows[0].SessionID != sid || rows[0].ConfigDir != work {
		t.Errorf("rows = %+v", rows)
	}
	if got := f.run("claude-profile", "which", "no-such-session"); got != exitFail {
		t.Errorf("unknown session: exit %d", got)
	}
}

func TestWhichPID(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(f.root, "proc", "4242", "environ"), "HOME=/h\x00CLAUDE_CONFIG_DIR="+work+"\x00", 0o644)
	f.write(filepath.Join(work, "sessions", "4242.json"), `{"pid":4242,"sessionId":"`+sid+`"}`, 0o644)
	f.write(filepath.Join(f.root, "proc", "77", "environ"), "HOME=/h\x00", 0o644)

	if got := f.run("claude-profile", "which", "4242"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if want := "work\tc@example.com\t" + sid + "\t" + work + "\n"; f.out.String() != want {
		t.Errorf("which 4242 = %q, want %q", f.out.String(), want)
	}
	if got := f.run("claude-profile", "which", "77"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if !strings.HasPrefix(f.out.String(), "default\tme@example.com\t-\t") {
		t.Errorf("which 77 = %q", f.out.String())
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `make test`
Expected: FAIL — `internal/cli` does not compile (`unknown field UID in struct literal`, `undefined: listOutput`, `undefined: whichRow`).

- [ ] **Step 3: Write the implementation**

`internal/cli/cli.go` (replaces the Task 1 file):

```go
// Package cli is claude-profile's command line. It is the only package that
// reads the process environment, arguments and terminal; the rest of the
// program takes them as parameters so it can be tested in isolation.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/tmpdir"
	"github.com/mithro/go-claude-profile/internal/usage"
)

// Version is set at build time with
// -ldflags "-X github.com/mithro/go-claude-profile/internal/cli.Version=...".
var Version = "dev"

const toolName = "claude-profile"

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// Deps is everything the program touches outside its own memory.
type Deps struct {
	Env    map[string]string
	UID    int
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Self is this executable's absolute, symlink-resolved path.
	Self string
	// ProcRoot is /proc; CgroupRoot is /sys/fs/cgroup.
	ProcRoot   string
	CgroupRoot string
	// Exec replaces the process image; it returns only on failure.
	Exec func(path string, argv, env []string) error
	// Run runs a command to completion.
	Run func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error
	// LookPath finds a program on the process's PATH.
	LookPath func(name string) (string, error)
	// OpenTTY opens the controlling terminal to read menu answers.
	OpenTTY func() (io.ReadCloser, error)
	// CheckTmp validates the temp directory.
	CheckTmp func(dir string, uid int) error
}

// OSDeps returns the real process's dependencies.
func OSDeps() Deps {
	self, err := os.Executable()
	if err == nil {
		if r, err := filepath.EvalSymlinks(self); err == nil {
			self = r
		}
	}
	return Deps{
		Env:        envMap(os.Environ()),
		UID:        os.Getuid(),
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Self:       self,
		ProcRoot:   "/proc",
		CgroupRoot: "/sys/fs/cgroup",
		Exec:       syscall.Exec,
		Run: func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Env = env
			cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
			return cmd.Run()
		},
		LookPath: exec.LookPath,
		OpenTTY:  func() (io.ReadCloser, error) { return os.Open("/dev/tty") },
		CheckTmp: tmpdir.Check,
	}
}

func envMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// RunMultiCall runs the program as invoked under argv0. Under its own name it
// is the management command; under any other name it is a launcher.
func RunMultiCall(argv0 string, args []string, d Deps) int {
	if name := filepath.Base(argv0); name != toolName {
		return runLauncher(name, args, d)
	}
	return runManagement(args, d)
}

// command is one management subcommand.
type command struct {
	name    string
	summary string
	run     func(args []string, d Deps) int
}

var commands []command

func register(c command) { commands = append(commands, c) }

func runManagement(args []string, d Deps) int {
	if len(args) == 0 {
		printHelp(d.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(d.Stdout, "%s %s\n", toolName, Version)
		return exitOK
	case "help", "--help", "-h":
		printHelp(d.Stdout)
		return exitOK
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:], d)
		}
	}
	return fail(d, usage.Errorf("unknown command %q (try %s help)", args[0], toolName))
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "usage: %s <command> [args]\n\n", toolName)
	fmt.Fprint(w, "Launches Claude Code in a chosen profile (config directory), inside a\n"+
		"resource-limited cgroup. Invoked under another name (a symlink), it is a\n"+
		"launcher: that name selects a config, and every argument goes to Claude.\n\n"+
		"commands:\n")
	cs := append([]command(nil), commands...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, c := range cs {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "  %-10s %s\n  %-10s %s\n", "help", "show this help", "version", "print the version")
}

// fail prints err and returns the exit status for it.
func fail(d Deps, err error) int {
	fmt.Fprintf(d.Stderr, "%s: %v\n", toolName, err)
	if usage.Is(err) {
		return exitUsage
	}
	return exitFail
}

func warn(d Deps, format string, a ...any) {
	fmt.Fprintf(d.Stderr, "%s: warning: %s\n", toolName, fmt.Sprintf(format, a...))
}

// world is the configuration and the discovered profiles.
type world struct {
	home     string
	cfg      *config.File
	profiles []profile.Profile
}

func load(d Deps) (*world, error) {
	home := d.Env["HOME"]
	if home == "" {
		return nil, errors.New("HOME is not set")
	}
	cfg, err := config.Load(config.Path(d.Env, home), home)
	if err != nil {
		return nil, usage.Errorf("%v", err)
	}
	ps, err := profile.Discover(home, cfg)
	if err != nil {
		return nil, err
	}
	return &world{home: home, cfg: cfg, profiles: ps}, nil
}

// parseFlags parses args; when ok is false, return code.
func parseFlags(fs *flag.FlagSet, args []string) (code int, ok bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, false
		}
		return exitUsage, false
	}
	return 0, true
}

func writeJSON(d Deps, v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fail(d, err)
	}
	fmt.Fprintf(d.Stdout, "%s\n", data)
	return exitOK
}

func profileNames(ps []profile.Profile) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}
```

`internal/cli/list.go`:

```go
package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/menu"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"list", "list accounts, profiles and configs [--json]", cmdList})
}

type listOutput struct {
	Accounts []string     `json:"accounts"`
	Profiles []profileOut `json:"profiles"`
	Configs  []configOut  `json:"configs"`
}

type profileOut struct {
	Name            string `json:"name"`
	Dir             string `json:"dir"`
	Default         bool   `json:"default"`
	Account         string `json:"account"`
	ExpectedAccount string `json:"expected_account,omitempty"`
	LoggedIn        bool   `json:"logged_in"`
	Note            string `json:"note,omitempty"`
}

type configOut struct {
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

func cmdList(args []string, d Deps) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail(d, usage.Errorf("list takes no arguments"))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	if *asJSON {
		return writeJSON(d, buildList(w))
	}
	for _, g := range menu.Build(w.profiles, w.cfg) {
		fmt.Fprintln(d.Stdout, g.Heading)
		for _, e := range g.Entries {
			if e.Item.Kind != menu.KindNew {
				fmt.Fprintf(d.Stdout, "  %s\n", e.Label)
			}
		}
	}
	return exitOK
}

func buildList(w *world) listOutput {
	out := listOutput{Accounts: []string{}, Profiles: []profileOut{}, Configs: []configOut{}}
	seen := map[string]bool{}
	add := func(a string) {
		if k := strings.ToLower(a); a != "" && !seen[k] {
			seen[k] = true
			out.Accounts = append(out.Accounts, a)
		}
	}
	for _, p := range w.profiles {
		add(p.Account)
	}
	for _, a := range w.cfg.Accounts {
		add(a)
	}
	slices.SortFunc(out.Accounts, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	for _, p := range w.profiles {
		out.Profiles = append(out.Profiles, profileOut{Name: p.Name, Dir: p.Dir, Default: p.Default, Account: p.Account,
			ExpectedAccount: p.Expected, LoggedIn: p.Account != "", Note: p.Note})
	}
	for _, n := range w.cfg.ConfigNames() {
		out.Configs = append(out.Configs, configOut{Name: n, Profile: w.cfg.Configs[n].Profile})
	}
	return out
}
```

`internal/cli/which.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/resume"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"which", "show the profile owning a session: which [--json] UUID|NAME|PID", cmdWhich})
}

type whichRow struct {
	Profile    string `json:"profile"`
	ConfigDir  string `json:"config_dir"`
	Account    string `json:"account"`
	SessionID  string `json:"session_id,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	PID        int    `json:"pid,omitempty"`
}

func cmdWhich(args []string, d Deps) int {
	fs := flag.NewFlagSet("which", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return fail(d, usage.Errorf("usage: %s which [--json] SESSION-UUID|SESSION-NAME|PID", toolName))
	}
	arg := fs.Arg(0)
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	var rows []whichRow
	if pid, err := strconv.Atoi(arg); err == nil && pid > 0 {
		row, err := whichPID(d, w, pid)
		if err != nil {
			return fail(d, err)
		}
		rows = []whichRow{row}
	} else {
		hits, err := resume.Lookup(w.profiles, arg)
		if err != nil {
			return fail(d, err)
		}
		if len(hits) == 0 {
			return fail(d, fmt.Errorf("no session %q in any profile", arg))
		}
		for _, h := range hits {
			p, _ := profile.Find(w.profiles, h.Profile)
			rows = append(rows, whichRow{Profile: p.Name, ConfigDir: p.Dir, Account: p.Account, SessionID: h.SessionID, Transcript: h.Transcript})
		}
	}
	if *asJSON {
		return writeJSON(d, rows)
	}
	for _, r := range rows {
		fmt.Fprintf(d.Stdout, "%s\t%s\t%s\t%s\n", dash(r.Profile), dash(r.Account), dash(r.SessionID), r.ConfigDir)
	}
	return exitOK
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// whichPID reads a process's CLAUDE_CONFIG_DIR to find its profile, and the
// profile's session registry for its session ID.
func whichPID(d Deps, w *world, pid int) (whichRow, error) {
	data, err := os.ReadFile(filepath.Join(d.ProcRoot, strconv.Itoa(pid), "environ"))
	if err != nil {
		return whichRow{}, err
	}
	var dir string
	for _, kv := range bytes.Split(data, []byte{0}) {
		if v, ok := bytes.CutPrefix(kv, []byte("CLAUDE_CONFIG_DIR=")); ok {
			dir = string(v)
		}
	}
	var p profile.Profile
	var ok bool
	if dir == "" {
		p, ok = profile.Find(w.profiles, "default")
		dir = filepath.Join(w.home, ".claude")
	} else {
		p, ok = profile.ByDir(w.profiles, dir)
	}
	if !ok {
		return whichRow{ConfigDir: dir, PID: pid}, nil
	}
	row := whichRow{Profile: p.Name, ConfigDir: p.Dir, Account: p.Account, PID: pid}
	if data, err := os.ReadFile(filepath.Join(p.Dir, "sessions", strconv.Itoa(pid)+".json")); err == nil {
		var e struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(data, &e) == nil {
			row.SessionID = e.SessionID
		}
	}
	return row, nil
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: all packages `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "Add list and which

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 10: The launcher, `run` and `new`

**Files:**
- Modify (replace): `internal/cli/launcher.go`
- Create: `internal/cli/launcher_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–9: `launch.Build`, `launch.Request`, `launch.WantScope`, `launch.ResolveClaude`, `launch.EnvList`, `managed.Sync`, `slice.Desired`, `slice.UnitDir`, `menu.*`, `profile.*`, `resume.Value`, `resume.Lookup`, `tmpdir.Ensure`.
- Produces: `runLauncher(name, args, d) int`; `syncSlices(d Deps, w *world) error` (also used by Task 11); `createProfile(d, w, name, account) error`.

- [ ] **Step 1: Write the failing test**

`internal/cli/launcher_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to see it fail**

Run: `make test`
Expected: FAIL — the launcher tests fail with `the launcher is not built yet` and `unknown command "new"`/`"run"`.

- [ ] **Step 3: Write the implementation**

`internal/cli/launcher.go` (replaces the Task 1 file):

```go
package cli

import (
	"bufio"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/menu"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/resume"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/tmpdir"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"run", "launch Claude: run [--profile P | --config C] [--] [claude args]", cmdRun})
	register(command{"new", "create a profile and log it in: new [--account EMAIL] NAME", cmdNew})
}

type launchOpts struct {
	invoked string // the name the program was invoked as
	profile string // run --profile
	config  string // run --config
	args    []string
}

func runLauncher(name string, args []string, d Deps) int {
	return launchClaude(d, launchOpts{invoked: name, args: args})
}

func cmdRun(args []string, d Deps) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	prof := fs.String("profile", "", "profile to launch in")
	cfgName := fs.String("config", "", "config to launch with")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if *prof != "" && *cfgName != "" {
		return fail(d, usage.Errorf("give --profile or --config, not both"))
	}
	return launchClaude(d, launchOpts{invoked: toolName, profile: *prof, config: *cfgName, args: fs.Args()})
}

func launchClaude(d Deps, o launchOpts) int {
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	profName, cfgName, err := selectProfile(d, w, o)
	if err != nil {
		return fail(d, err)
	}
	p, ok := profile.Find(w.profiles, profName)
	if !ok {
		return fail(d, usage.Errorf("no profile named %q (see %s list)", profName, toolName))
	}
	var c *config.Config
	if cfgName != "" {
		cc := w.cfg.Configs[cfgName]
		c = &cc
	}
	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	if err != nil {
		return fail(d, err)
	}
	if err := d.CheckTmp(w.cfg.TmpDir, d.UID); err != nil {
		return fail(d, usage.Errorf("%v", err))
	}
	if err := tmpdir.Ensure(w.cfg.TmpDir); err != nil {
		return fail(d, err)
	}
	cgroup, _ := os.ReadFile(filepath.Join(d.ProcRoot, "self", "cgroup"))
	systemdRun := ""
	if launch.WantScope(d.Env, string(cgroup), p.Name) {
		if systemdRun, err = d.LookPath("systemd-run"); err != nil {
			return fail(d, fmt.Errorf("systemd-run: %w (set %s=1 to run without limits)", err, launch.NoCgroupEnv))
		}
		if err := syncSlices(d, w); err != nil {
			return fail(d, err)
		}
	}
	plan, err := launch.Build(launch.Request{
		Env: d.Env, Profile: p, ConfigName: cfgName, Config: c, Args: o.args,
		Claude: claude, SystemdRun: systemdRun, TmpDir: w.cfg.TmpDir,
		Limits: w.cfg.SessionLimitsFor(p.Name, cfgName),
	})
	if err != nil {
		return fail(d, err)
	}
	for _, msg := range plan.Warnings {
		warn(d, "%s", msg)
	}
	err = d.Exec(plan.Argv[0], plan.Argv, plan.Env)
	return fail(d, fmt.Errorf("exec %s: %w", plan.Argv[0], err))
}

// selectProfile applies the spec's order: run --profile/--config, a config
// named like the invoked name, the profile owning a --resume session, an
// inherited CLAUDE_PROFILE, then the menu.
func selectProfile(d Deps, w *world, o launchOpts) (profName, cfgName string, err error) {
	profName, cfgName = o.profile, o.config
	if cfgName == "" && o.invoked != toolName {
		if _, ok := w.cfg.Configs[o.invoked]; ok {
			cfgName = o.invoked
		}
	}
	if cfgName != "" {
		c, ok := w.cfg.Configs[cfgName]
		if !ok {
			return "", "", usage.Errorf("no config named %q", cfgName)
		}
		profName = c.Profile
	}
	if profName != "" {
		if _, ok := profile.Find(w.profiles, profName); !ok {
			return "", "", usage.Errorf("no profile named %q (see %s list)", profName, toolName)
		}
	}
	v, resuming := resume.Value(o.args)
	if !resuming {
		if profName != "" {
			return profName, cfgName, nil
		}
		if inherited := d.Env["CLAUDE_PROFILE"]; inherited != "" {
			if _, ok := profile.Find(w.profiles, inherited); ok {
				return inherited, "", nil
			}
		}
		return chooseInteractively(d, w, "")
	}
	hits, err := resume.Lookup(w.profiles, v)
	if err != nil {
		return "", "", err
	}
	owners := hitOwners(hits)
	switch {
	case profName != "":
		if len(owners) > 0 && !slices.Contains(owners, profName) {
			return "", "", usage.Errorf("session %s belongs to profile %s, not %s", v, strings.Join(owners, ", "), profName)
		}
		return profName, cfgName, nil
	case len(owners) == 1:
		return owners[0], "", nil
	case len(owners) == 0:
		return chooseInteractively(d, w, fmt.Sprintf("No profile has a session %q.", v))
	default:
		return chooseInteractively(d, w, fmt.Sprintf("Session %q exists in several profiles: %s.", v, strings.Join(owners, ", ")))
	}
}

func hitOwners(hits []resume.Hit) []string {
	var owners []string
	for _, h := range hits {
		if !slices.Contains(owners, h.Profile) {
			owners = append(owners, h.Profile)
		}
	}
	slices.Sort(owners)
	return owners
}

func chooseInteractively(d Deps, w *world, note string) (string, string, error) {
	tty, err := d.OpenTTY()
	if err != nil {
		msg := fmt.Sprintf("no profile selected and no terminal for the menu; use %s run --profile NAME (profiles: %s)",
			toolName, strings.Join(profileNames(w.profiles), ", "))
		if note != "" {
			msg = note + " " + msg
		}
		return "", "", usage.Errorf("%s", msg)
	}
	defer tty.Close()
	br := bufio.NewReader(tty)
	statePath := menu.StatePath(d.Env, w.home)
	item, err := menu.Choose(menu.Build(w.profiles, w.cfg), br, d.Stderr, note, menu.LoadLast(statePath))
	if err != nil {
		return "", "", err
	}
	if item.Kind == menu.KindNew {
		name, err := menu.Prompt(br, d.Stderr, "Name for the new profile: ")
		if err != nil {
			return "", "", err
		}
		if err := createProfile(d, w, name, item.Account); err != nil {
			return "", "", err
		}
		item = menu.Item{Kind: menu.KindProfile, Profile: name}
	}
	if err := menu.SaveLast(statePath, item.Key()); err != nil {
		warn(d, "could not remember the choice: %v", err)
	}
	return item.Profile, item.Config, nil
}

// syncSlices brings the slice units up to date and reloads systemd if any
// changed.
func syncSlices(d Deps, w *world) error {
	res, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), false)
	if err != nil {
		return err
	}
	for _, f := range res.Foreign {
		warn(d, "%s is not managed by %s; leaving it alone", f, toolName)
	}
	if res.Changed() {
		return d.Run([]string{"systemctl", "--user", "daemon-reload"}, launch.EnvList(d.Env), nil, d.Stderr, d.Stderr)
	}
	return nil
}

func cmdNew(args []string, d Deps) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	account := fs.String("account", "", "the account (email) to log in as")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fail(d, usage.Errorf("usage: %s new [--account EMAIL] NAME", toolName))
	}
	name := rest[0]
	if code, ok := parseFlags(fs, rest[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail(d, usage.Errorf("usage: %s new [--account EMAIL] NAME", toolName))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	if err := createProfile(d, w, name, *account); err != nil {
		return fail(d, err)
	}
	return exitOK
}

// createProfile makes ~/.claude-<name> and runs Claude's own login in it. No
// credentials are copied from anywhere.
func createProfile(d Deps, w *world, name, account string) error {
	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	if err != nil {
		return err
	}
	dir, err := profile.Create(w.home, name)
	if err != nil {
		return err
	}
	argv := []string{claude, "auth", "login", "--claudeai"}
	if account != "" {
		argv = append(argv, "--email", account)
	}
	env := maps.Clone(d.Env)
	env["CLAUDE_CONFIG_DIR"] = dir
	fmt.Fprintf(d.Stderr, "Logging in profile %s (%s)...\n", name, dir)
	if err := d.Run(argv, launch.EnvList(env), d.Stdin, d.Stdout, d.Stderr); err != nil {
		if os.Remove(dir) == nil {
			return fmt.Errorf("login for profile %s failed: %w (removed the empty %s)", name, err, dir)
		}
		return fmt.Errorf("login for profile %s failed: %w (%s is left as it is; retry with CLAUDE_CONFIG_DIR=%s claude auth login)", name, err, dir, dir)
	}
	got, note := profile.ReadAccount(filepath.Join(dir, ".claude.json"))
	switch {
	case got == "":
		return fmt.Errorf("profile %s is still not logged in after the login %s", name, note)
	case account != "" && !strings.EqualFold(got, account):
		warn(d, "profile %s logged in as %s, not %s", name, got, account)
	default:
		fmt.Fprintf(d.Stderr, "Profile %s is logged in as %s.\n", name, got)
	}
	w.profiles, err = profile.Discover(w.home, w.cfg)
	return err
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test`
Expected: all packages `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "Launch Claude in the selected profile, and add run and new

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 11: `setup` and `doctor`

**Files:**
- Create: `internal/setup/setup.go`, `internal/setup/setup_test.go`, `internal/cli/setup.go`, `internal/cli/doctor.go`, `internal/cli/setup_test.go`

**Interfaces:**
- Consumes: `managed.Sync`, `managed.Remove`, `managed.Marker`, `slice.Desired`, `slice.UnitDir`, `launch.ResolveClaude`, `launch.EnvList`, `profile.Find`, `tmpdir.Ensure`, `syncSlices` is not reused here (setup reports per file).
- Produces: `setup.TmpfilesName`, `setup.TmpfilesDir(env, home) string`, `setup.TmpfilesContent(tmpDir string) string`, `type setup.LinkResult struct{ Created, Unchanged, Foreign []string }`, `setup.SyncLinks(dir, self string, names []string, dryRun bool) (LinkResult, error)`, `setup.RemoveLinks(dir, self string, names []string) ([]string, error)`; commands `setup install|status|uninstall` and `doctor`.

- [ ] **Step 1: Write the failing tests**

`internal/setup/setup_test.go`:

```go
package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/managed"
)

func TestTmpfiles(t *testing.T) {
	if got := TmpfilesDir(map[string]string{}, "/h"); got != "/h/.config/user-tmpfiles.d" {
		t.Errorf("TmpfilesDir = %q", got)
	}
	got := TmpfilesContent("/h/.claude-tmp")
	if got != managed.Marker+"\nd /h/.claude-tmp 0700 - - 14d\n" {
		t.Errorf("TmpfilesContent = %q", got)
	}
}

func TestLinks(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "claude-profile")
	if err := os.WriteFile(self, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "claude-mine"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink(self, filepath.Join(dir, "claude-ok"))
	names := []string{"claude-new", "claude-ok", "claude-mine"}

	dry, err := SyncLinks(dir, self, names, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "claude-new")); err == nil {
		t.Error("dry run created a link")
	}
	res, err := SyncLinks(dir, self, names, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dry, res) {
		t.Errorf("dry %+v != real %+v", dry, res)
	}
	j := func(n string) string { return filepath.Join(dir, n) }
	want := LinkResult{Created: []string{j("claude-new")}, Unchanged: []string{j("claude-ok")}, Foreign: []string{j("claude-mine")}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("SyncLinks = %+v, want %+v", res, want)
	}
	if target, _ := os.Readlink(j("claude-new")); target != self {
		t.Errorf("claude-new -> %q", target)
	}
	removed, err := RemoveLinks(dir, self, names)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{j("claude-new"), j("claude-ok")}) {
		t.Errorf("RemoveLinks = %q", removed)
	}
	if data, _ := os.ReadFile(j("claude-mine")); !strings.HasPrefix(string(data), "#!") {
		t.Error("RemoveLinks touched a file it does not own")
	}
}
```

`internal/cli/setup_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

	f.runs = nil
	if code := f.run("claude-profile", "setup", "install"); code != exitOK || len(f.runs) != 0 {
		t.Errorf("second install: exit %d, runs %q", code, f.runs)
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
```

`RemoveLinks` removes only `claude-new` and `claude-ok` (the symlinks to self), in the order of `names`; `claude-mine` is the user's own file.

- [ ] **Step 2: Run the tests to see them fail**

Run: `make test`
Expected: FAIL — `internal/setup` does not compile; `unknown command "setup"`.

- [ ] **Step 3: Write the implementation**

`internal/setup/setup.go`:

```go
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
```

`internal/cli/setup.go`:

```go
package cli

import (
	"fmt"

	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/setup"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/tmpdir"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"setup", "manage per-user files: setup install|status|uninstall", cmdSetup})
}

func cmdSetup(args []string, d Deps) int {
	if len(args) != 1 {
		return fail(d, usage.Errorf("usage: %s setup install|status|uninstall", toolName))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	switch args[0] {
	case "install":
		return setupApply(d, w, false)
	case "status":
		return setupApply(d, w, true)
	case "uninstall":
		return setupUninstall(d, w)
	}
	return fail(d, usage.Errorf("usage: %s setup install|status|uninstall", toolName))
}

// setupApply installs (or, with dryRun, reports) the slice units, the
// tmpfiles.d rule and the config symlinks. With dryRun it fails if anything
// would change; either way it fails if a path is not ours.
func setupApply(d Deps, w *world, dryRun bool) int {
	if err := d.CheckTmp(w.cfg.TmpDir, d.UID); err != nil {
		return fail(d, usage.Errorf("%v", err))
	}
	if !dryRun {
		if err := tmpdir.Ensure(w.cfg.TmpDir); err != nil {
			return fail(d, err)
		}
	}
	units, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), dryRun)
	if err != nil {
		return fail(d, err)
	}
	rule, err := managed.Sync(setup.TmpfilesDir(d.Env, w.home),
		map[string]string{setup.TmpfilesName: setup.TmpfilesContent(w.cfg.TmpDir)}, dryRun)
	if err != nil {
		return fail(d, err)
	}
	links, err := setup.SyncLinks(w.cfg.LinkDir, d.Self, w.cfg.ConfigNames(), dryRun)
	if err != nil {
		return fail(d, err)
	}
	written, linked := "wrote", "linked"
	if dryRun {
		written, linked = "would write", "would link"
	}
	report := func(status string, paths []string) {
		for _, p := range paths {
			fmt.Fprintf(d.Stdout, "%-12s %s\n", status, p)
		}
	}
	for _, r := range []managed.Result{units, rule} {
		report(written, r.Written)
		report("ok", r.Unchanged)
		report("not managed", r.Foreign)
	}
	report(linked, links.Created)
	report("ok", links.Unchanged)
	report("not managed", links.Foreign)

	if !dryRun && units.Changed() {
		if err := d.Run([]string{"systemctl", "--user", "daemon-reload"}, launch.EnvList(d.Env), nil, d.Stderr, d.Stderr); err != nil {
			return fail(d, err)
		}
	}
	bad := len(units.Foreign)+len(rule.Foreign)+len(links.Foreign) > 0
	if dryRun && (units.Changed() || rule.Changed() || len(links.Created) > 0) {
		bad = true
	}
	if bad {
		return exitFail
	}
	return exitOK
}

func setupUninstall(d Deps, w *world) int {
	units, err := managed.Remove(slice.UnitDir(d.Env, w.home), "claude*.slice")
	if err != nil {
		return fail(d, err)
	}
	rules, err := managed.Remove(setup.TmpfilesDir(d.Env, w.home), setup.TmpfilesName)
	if err != nil {
		return fail(d, err)
	}
	links, err := setup.RemoveLinks(w.cfg.LinkDir, d.Self, w.cfg.ConfigNames())
	if err != nil {
		return fail(d, err)
	}
	for _, group := range [][]string{units, rules, links} {
		for _, p := range group {
			fmt.Fprintf(d.Stdout, "%-12s %s\n", "removed", p)
		}
	}
	fmt.Fprintf(d.Stdout, "The temp directory %s is left in place.\n", w.cfg.TmpDir)
	if len(units) > 0 {
		if err := d.Run([]string{"systemctl", "--user", "daemon-reload"}, launch.EnvList(d.Env), nil, d.Stderr, d.Stderr); err != nil {
			return fail(d, err)
		}
	}
	return exitOK
}
```

`internal/cli/doctor.go`:

```go
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"doctor", "check that launching will work", cmdDoctor})
}

func cmdDoctor(args []string, d Deps) int {
	if len(args) != 0 {
		return fail(d, usage.Errorf("doctor takes no arguments"))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(d.Stdout, "FAIL  %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(d.Stdout, "ok    %s\n", name)
	}

	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	check("claude binary "+claude, err)
	if path, err := d.LookPath("systemd-run"); err != nil {
		check("systemd-run --user", err)
	} else {
		check("systemd-run --user", d.Run([]string{path, "--user", "--scope", "--collect", "--quiet", "true"},
			launch.EnvList(d.Env), nil, d.Stdout, d.Stdout))
	}
	check("cgroup controllers delegated", checkControllers(d))
	check("temp directory "+w.cfg.TmpDir, d.CheckTmp(w.cfg.TmpDir, d.UID))
	check("slice units", checkSlices(d, w))
	for _, p := range w.profiles {
		switch {
		case p.Mismatch():
			check("profile "+p.Name, fmt.Errorf("logged in as %s, expected %s", p.Account, p.Expected))
		case p.Account == "":
			fmt.Fprintf(d.Stdout, "note  profile %s is not logged in %s\n", p.Name, p.Note)
		default:
			check("profile "+p.Name+" ("+p.Account+")", nil)
		}
	}
	for _, name := range w.cfg.ConfigNames() {
		var err error
		if c := w.cfg.Configs[name]; !hasProfile(w.profiles, c.Profile) {
			err = fmt.Errorf("names profile %q, which does not exist", c.Profile)
		}
		check("config "+name, err)
	}
	if failed {
		return exitFail
	}
	return exitOK
}

func hasProfile(ps []profile.Profile, name string) bool {
	_, ok := profile.Find(ps, name)
	return ok
}

func checkControllers(d Deps) error {
	path := filepath.Join(d.CgroupRoot, "user.slice", fmt.Sprintf("user-%d.slice", d.UID),
		fmt.Sprintf("user@%d.service", d.UID), "cgroup.controllers")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	have := strings.Fields(string(data))
	var missing []string
	for _, c := range []string{"cpu", "memory", "pids"} {
		if !slices.Contains(have, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("not delegated to the user manager: %s", strings.Join(missing, ", "))
	}
	return nil
}

func checkSlices(d Deps, w *world) error {
	res, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), true)
	if err != nil {
		return err
	}
	if len(res.Foreign) > 0 {
		return fmt.Errorf("not managed by %s: %s", toolName, strings.Join(res.Foreign, ", "))
	}
	if res.Changed() {
		return errors.New("out of date; run claude-profile setup install")
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `make test && make vet && make fmt-check`
Expected: all packages `ok`; vet and fmt-check print nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/setup internal/cli
git commit -m "Add setup and doctor

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 12: Live test against the real systemd user manager

**Files:**
- Create: `internal/launch/live_test.go`

**Interfaces:**
- Consumes: `launch.Build`, `launch.Request`, `slice.UnitName`, `config.Limits`, `profile.Profile`.

- [ ] **Step 1: Write the test**

`internal/launch/live_test.go`:

```go
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
```

- [ ] **Step 2: Run it**

Run: `make test-live`
Expected: `ok  github.com/mithro/go-claude-profile/internal/launch`. Then confirm the throwaway slice is gone:

```bash
systemctl --user list-units --all 'claude-live*' --no-pager --no-legend
```

Expected: no output. (`claude.slice` itself may remain listed under `claude*`; leave it.)

- [ ] **Step 3: Make sure the default suite skips it**

Run: `make test`
Expected: all `ok`, and `go test` does not start any scope (the file has the `systemdlive` build tag).

- [ ] **Step 4: Commit**

```bash
git add internal/launch/live_test.go
git commit -m "Test a launch against the real systemd user manager

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 13: Packaging, CI and README

**Files:**
- Create: `nfpm.yaml`, `.github/apt-packaging.toml`, `.github/workflows/deb.yml`, `packaging/release-assets.sh`, `packaging/release-assets-test.sh`, `README.md`

- [ ] **Step 1: Copy the release-asset scripts unchanged from go-tmux-saver**

```bash
mkdir -p packaging .github/workflows
git -C ~/github/mithro/go-tmux-saver show origin/main:packaging/release-assets.sh > packaging/release-assets.sh
git -C ~/github/mithro/go-tmux-saver show origin/main:packaging/release-assets-test.sh > packaging/release-assets-test.sh
chmod 0755 packaging/release-assets.sh packaging/release-assets-test.sh
TMPDIR=$PWD/tmp sh packaging/release-assets-test.sh
```

Expected: the five asset names, `OK` lines from `sha256sum -c`, then `release-assets-test: ok`.

- [ ] **Step 2: Write `.github/apt-packaging.toml` and `nfpm.yaml`**

`.github/apt-packaging.toml`:

```toml
# How this repository is packaged, and why it differs from the defaults.
# See https://github.com/mithro/apt-repo-action/blob/main/docs/packaging.md

kind = "B"                  # our own code: a Go program packaged with nfpm (nfpm.yaml)
```

`nfpm.yaml`:

```yaml
# nfpm packaging config — https://nfpm.goreleaser.com/configuration/
# VERSION and ARCH are expanded from the environment by nfpm at package time
# (nfpm does not expand them inside contents[].src, so the binary is always
# packaged from the fixed path ./dist/claude-profile — build it there first):
#   VERSION=0.1.post3~deb13 ARCH=amd64 nfpm package -p deb -f nfpm.yaml -t dist/
# deb.yml gets VERSION from mithro/apt-repo-action's deb-version action.
name: claude-profile
arch: ${ARCH}
platform: linux
version: ${VERSION}
# Keep the Debian version exactly as given (0.1.post3~deb13); without this
# nfpm re-renders it as semver.
version_schema: none
section: utils
priority: optional
maintainer: Tim 'mithro' Ansell <me@mith.ro>
description: |
  Launch Claude Code in per-account profiles.
  Picks the Claude Code config directory (and so the account) for a launch,
  including the one that owns a --resume session, applies named launch
  presets, runs Claude inside a resource-limited systemd user scope, and
  points its temp files at disk instead of a RAM-backed /tmp.
vendor: mithro
homepage: https://github.com/mithro/go-claude-profile
license: Apache-2.0
depends:
  - systemd
contents:
  - src: ./dist/claude-profile
    dst: /usr/bin/claude-profile
    file_info:
      mode: 0755
  - src: ./README.md
    dst: /usr/share/doc/claude-profile/README.md
  - src: ./LICENSE
    dst: /usr/share/doc/claude-profile/copyright
```

- [ ] **Step 3: Write `.github/workflows/deb.yml`**

This is go-tmux-saver's workflow with the binary, module, test steps and descriptions changed.

```yaml
name: Debian packages

# Every push to main builds and publishes: the tests, then a static binary per
# architecture packaged with nfpm for every suite, the signed apt repository at
# https://mith.ro/go-claude-profile/, and a GitHub Release of the build. Pull
# requests build the same packages as previews (versions ending ~pr<P>) and
# publish nothing. The conventions are mithro/apt-repo-action's
# docs/packaging.md.
#
# Versions come from git through the shared scripts/deb-version.py
# (mithro/apt-repo-action's deb-version action): <X.Y>.post<N> after the last
# hand-made vX.Y tag, plus ~deb<R> per suite and ~pr<P> on a pull request. CI
# never creates a vX.Y tag.

on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: deb-${{ github.ref }}
  # A newer push to a pull request cancels the older build. On the default
  # branch a running publish is never cancelled: the newest push waits for it,
  # and replaces any older push still waiting (it contains that one's commits).
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}

env:
  NFPM_VERSION: 2.41.1
  GO_MODULE: github.com/mithro/go-claude-profile

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go test -race ./...
      # The release job only runs on the default branch; this checks its
      # asset naming on every build (see packaging/release-assets.sh).
      - name: Release asset names
        run: sh packaging/release-assets-test.sh

  # One job per architecture: the static binary is built once, then packaged
  # for each of its suites, since ~deb<R> gives every suite its own version.
  # Go cross-compiles, so every job builds natively; the runner is the one
  # that can run the architecture's install test.
  build-deb:
    name: build-deb (${{ join(matrix.suites, ' ') }} ${{ matrix.arch }})
    needs: test
    runs-on: ${{ matrix.runner }}
    strategy:
      fail-fast: false
      matrix:
        include:
          # `arch` is the Debian name; `goarch`/`goarm` what Go wants;
          # `platform` the docker platform the install test runs.
          - {arch: amd64, goarch: amd64, goarm: "", runner: ubuntu-24.04, platform: linux/amd64,
             suites: [trixie, forky, sid]}
          - {arch: i386, goarch: "386", goarm: "", runner: ubuntu-24.04, platform: linux/386,
             suites: [trixie, forky, sid]}
          - {arch: arm64, goarch: arm64, goarm: "", runner: ubuntu-24.04-arm, platform: linux/arm64,
             suites: [trixie, forky, sid]}
          # ARMv6 (GOARM=6), so the one armhf binary also runs on Raspbian's
          # ARMv6 Raspberry Pis. The arm64 runners execute armhf natively.
          - {arch: armhf, goarch: arm, goarm: "6", runner: ubuntu-24.04-arm, platform: linux/arm/v7,
             suites: [trixie, forky, sid, raspbian-trixie, raspbian-forky]}
          # No riscv64 runner: its install test runs under QEMU.
          - {arch: riscv64, goarch: riscv64, goarm: "", runner: ubuntu-24.04, platform: linux/riscv64,
             suites: [trixie, forky, sid]}
    steps:
      - name: Check out
        uses: actions/checkout@v5
        with:
          fetch-depth: 0  # the version needs history and tags

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      # One step per suite: the shared action versions one suite at a time.
      - name: Version (trixie)
        id: v-trixie
        if: contains(matrix.suites, 'trixie')
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: trixie
      - name: Version (forky)
        id: v-forky
        if: contains(matrix.suites, 'forky')
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: forky
      - name: Version (sid)
        id: v-sid
        if: contains(matrix.suites, 'sid')
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: sid
      - name: Version (raspbian-trixie)
        id: v-raspbian-trixie
        if: contains(matrix.suites, 'raspbian-trixie')
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: raspbian-trixie
      - name: Version (raspbian-forky)
        id: v-raspbian-forky
        if: contains(matrix.suites, 'raspbian-forky')
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: raspbian-forky

      - name: Build
        env:
          ARCH: ${{ matrix.arch }}
          GOARCH: ${{ matrix.goarch }}
          GOARM: ${{ matrix.goarm }}
          SUITES: ${{ join(matrix.suites, ' ') }}
          # The binary's own version is the suite-independent one: sid's.
          BINARY_VERSION: ${{ steps.v-sid.outputs.version }}
          V_trixie: ${{ steps.v-trixie.outputs.version }}
          V_forky: ${{ steps.v-forky.outputs.version }}
          V_sid: ${{ steps.v-sid.outputs.version }}
          V_raspbian_trixie: ${{ steps.v-raspbian-trixie.outputs.version }}
          V_raspbian_forky: ${{ steps.v-raspbian-forky.outputs.version }}
        run: |
          set -euo pipefail
          mkdir -p dist
          CGO_ENABLED=0 GOOS=linux go build -trimpath \
            -ldflags "-s -w -X ${GO_MODULE}/internal/cli.Version=${BINARY_VERSION}" \
            -o dist/claude-profile ./cmd/claude-profile
          curl -fsSL -o "$RUNNER_TEMP/nfpm.deb" \
            "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/nfpm_${NFPM_VERSION}_$(dpkg --print-architecture).deb"
          sudo dpkg -i "$RUNNER_TEMP/nfpm.deb"
          for suite in $SUITES; do
            var="V_${suite//-/_}"
            mkdir -p "built-debs/$suite"
            VERSION="${!var}" nfpm package -p deb -f nfpm.yaml -t "built-debs/$suite/"
            dpkg-deb -f "built-debs/$suite"/*.deb Package Version Architecture
          done
          cp dist/claude-profile "dist/claude-profile-linux-$ARCH"

      - name: Install test
        env:
          ARCH: ${{ matrix.arch }}
          PLATFORM: ${{ matrix.platform }}
          SUITES: ${{ join(matrix.suites, ' ') }}
          BINARY_VERSION: ${{ steps.v-sid.outputs.version }}
        run: |
          set -euo pipefail
          if [ "$ARCH" = riscv64 ]; then
            docker run --privileged --rm tonistiigi/binfmt --install riscv64
          fi
          for suite in $SUITES; do
            # A Raspbian suite's package is tested in Debian armhf of the same
            # codename: it is the same ARMv6 binary.
            codename="${suite#raspbian-}"
            echo "::group::$suite ($PLATFORM)"
            # --platform always: after another architecture's container, the
            # local debian:<codename> tag may point at that image instead.
            docker run --rm --platform "$PLATFORM" -v "$PWD/built-debs/$suite:/debs:ro" \
              -e BINARY_VERSION "debian:$codename" sh -ec '
                apt-get update
                apt-get install -y /debs/*.deb
                claude-profile --version
                test "$(claude-profile --version)" = "claude-profile $BINARY_VERSION"'
            echo "::endgroup::"
          done

      - name: Upload (trixie)
        if: contains(matrix.suites, 'trixie')
        uses: actions/upload-artifact@v7
        with:
          name: debs-trixie-${{ matrix.arch }}
          path: built-debs/trixie/*.deb
          if-no-files-found: error
          retention-days: 14
      - name: Upload (forky)
        if: contains(matrix.suites, 'forky')
        uses: actions/upload-artifact@v7
        with:
          name: debs-forky-${{ matrix.arch }}
          path: built-debs/forky/*.deb
          if-no-files-found: error
          retention-days: 14
      - name: Upload (sid)
        if: contains(matrix.suites, 'sid')
        uses: actions/upload-artifact@v7
        with:
          name: debs-sid-${{ matrix.arch }}
          path: built-debs/sid/*.deb
          if-no-files-found: error
          retention-days: 14
      - name: Upload (raspbian-trixie)
        if: contains(matrix.suites, 'raspbian-trixie')
        uses: actions/upload-artifact@v7
        with:
          name: debs-raspbian-trixie-${{ matrix.arch }}
          path: built-debs/raspbian-trixie/*.deb
          if-no-files-found: error
          retention-days: 14
      - name: Upload (raspbian-forky)
        if: contains(matrix.suites, 'raspbian-forky')
        uses: actions/upload-artifact@v7
        with:
          name: debs-raspbian-forky-${{ matrix.arch }}
          path: built-debs/raspbian-forky/*.deb
          if-no-files-found: error
          retention-days: 14
      - name: Upload the binary
        uses: actions/upload-artifact@v7
        with:
          name: bin-${{ matrix.arch }}
          path: dist/claude-profile-linux-${{ matrix.arch }}
          if-no-files-found: error
          retention-days: 14

  publish-apt:
    if: github.event_name != 'pull_request' && github.ref_name == github.event.repository.default_branch
    needs: build-deb
    permissions:
      contents: read
      pages: write
      id-token: write
    uses: mithro/apt-repo-action/.github/workflows/publish-apt.yml@main
    with:
      suites: "trixie forky sid raspbian-trixie raspbian-forky"
      description: "Launch Claude Code in per-account profiles"
    secrets:
      gpg-private-key: ${{ secrets.APT_GPG_PRIVATE_KEY }}

  # A GitHub Release per build (docs/packaging.md, "GitHub Releases"): tagged
  # build-<version>, never v* (vX.Y tags are made by hand), with every
  # suite's .deb as <suite>_<file>.deb, the static binaries and SHA256SUMS.
  release:
    if: github.event_name != 'pull_request' && github.ref_name == github.event.repository.default_branch
    # After the publish: a build whose apt repository didn't deploy isn't
    # released either, and a re-run of the workflow does both.
    needs: [build-deb, publish-apt]
    runs-on: ubuntu-latest
    permissions:
      contents: write   # create the tag and the GitHub Release
    steps:
      - name: Check out
        uses: actions/checkout@v5
        with:
          fetch-depth: 0
      - name: Version
        id: v
        uses: mithro/apt-repo-action/deb-version@main
        with:
          suite: sid
      # Only the packages and binaries: since release waits for publish-apt,
      # the run also holds the github-pages artifact (the whole apt site).
      # A pattern keeps one directory per artifact (artifacts/<name>/).
      - uses: actions/download-artifact@v4
        with:
          pattern: debs-*
          path: artifacts/
      - uses: actions/download-artifact@v4
        with:
          pattern: bin*
          path: artifacts/
      # Named as GitHub stores them, so SHA256SUMS matches what's downloaded.
      - name: Collect the assets
        run: |
          set -euo pipefail
          sh packaging/release-assets.sh artifacts release artifacts/bin-*/claude-profile-linux-*
          cat release/SHA256SUMS
      - name: Create the GitHub Release
        env:
          GH_TOKEN: ${{ github.token }}
          TAG: build-${{ steps.v.outputs.version }}
        run: |
          set -euo pipefail
          if gh release view "$TAG" > /dev/null; then
            gh release upload "$TAG" release/* --clobber
          else
            gh release create "$TAG" release/* --target "$GITHUB_SHA" --title "$TAG" \
              --notes "Build of $GITHUB_SHA: static linux binaries, Debian packages for every suite (<suite>_<file>.deb) and SHA256SUMS. Apt repository: https://mith.ro/go-claude-profile/"
          fi
```

- [ ] **Step 4: Write `README.md`**

````markdown
# claude-profile

Launch [Claude Code](https://claude.com/claude-code) in the right **profile**
(a Claude Code config directory with its own login) for the right
**account**, inside a resource-limited systemd user scope, with its temp
files on disk instead of a RAM-backed `/tmp`.

- `claude-profile run -- --resume <id>` finds which profile owns the session
  and launches it there.
- With nothing to go on, it shows a menu of profiles grouped by the account
  each is logged into.
- **Configs** are named sets of launch arguments (model, environment, `PATH`
  prefix, limits). A symlink named after a config is a ready-made launcher:
  `claude-ac-coordinator` → `claude-profile`.

## Install

```
sudo install -d -m0755 /etc/apt/keyrings
curl -fsSL https://mith.ro/go-claude-profile/go-claude-profile.gpg | sudo tee /etc/apt/keyrings/go-claude-profile.gpg > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/go-claude-profile.gpg] https://mith.ro/go-claude-profile/trixie/ ./" \
  | sudo tee /etc/apt/sources.list.d/go-claude-profile.list
sudo apt update
sudo apt install claude-profile
```

The suites are `trixie`, `forky` and `sid`, plus `raspbian-trixie` and
`raspbian-forky`; use the one matching your system in the `deb` line.

`claude-profile version` prints the version without the suite suffix.

Every push to `main` is a build: it publishes the packages to the apt
repository and creates a GitHub Release tagged `build-<version>`. The
`vX.Y` tags are made by hand.

Then, once per user:

```
claude-profile setup install
claude-profile doctor
```

### Build from source

```
make build      # ./claude-profile
```

## Usage

| Command | What it does |
|---|---|
| `claude-profile run [--profile P \| --config C] [--] [claude args]` | Launch Claude. |
| `claude-profile list [--json]` | Accounts, profiles, config directories, configs. |
| `claude-profile which [--json] <session-uuid \| session-name \| pid>` | The profile, config directory and account that own a session or a running Claude. |
| `claude-profile new [--account EMAIL] NAME` | Create `~/.claude-NAME` and run Claude's own login in it. |
| `claude-profile setup install \| status \| uninstall` | Per-user files: slice units, the temp-dir cleanup rule, config symlinks. |
| `claude-profile doctor` | Check that launching will work. |

Invoked under any other name, the binary is a launcher and passes every
argument to Claude. If the name is a config's name, that config is used.

### How the profile is chosen

1. `run --profile` or `run --config`.
2. A config named like the command (the symlink).
3. For `--resume <id>`, `--resume=<id>` or `-r <id>`: the profile that owns
   that session, by UUID or by session name.
4. `CLAUDE_PROFILE` inherited from the environment (a launch from inside a
   wrapped session).
5. The menu. Without a terminal, the launch stops with an error listing the
   profiles.

If 1 or 2 picked a profile but the resumed session belongs to another one,
the launch stops with an error naming both.

## Profiles and accounts

- `~/.claude` is the profile `default`; it is launched with
  `CLAUDE_CONFIG_DIR` **unset**, so Claude reads `~/.claude.json`.
- Every `~/.claude-<name>` holding `.claude.json` or `.credentials.json` is
  the profile `<name>`, launched with `CLAUDE_CONFIG_DIR=~/.claude-<name>`.
- A profile's account is read from `oauthAccount.emailAddress` in its
  `.claude.json` every time. Nothing stores it, so a profile that gets
  logged into another account simply moves to that account's group.
- Credentials are never copied between profiles: each one does its own
  `/login`.

## Configuration

`~/.config/claude-profile/config.json` (or under `$XDG_CONFIG_HOME`). It is
optional; unknown keys are an error.

```json
{
  "accounts": ["me@example.com", "claude@example.com", "claude@example.org"],
  "ignore_profiles": ["old"],
  "profiles": {
    "west-corp": {"account": "claude@example.org", "slice_limits": {"MemoryMax": "24G"}}
  },
  "configs": {
    "claude-ac-coordinator": {
      "profile": "west-corp",
      "args": ["--model", "fable", "--name", "ac-coord"],
      "env": {"AC_SESSION": "ac-coord"},
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "opus"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"]
    },
    "claude-ac": {
      "profile": "west-corp",
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"],
      "name_arg": {"env": "AC_SESSION", "prefix": "ac-"}
    }
  }
}
```

| Key | Meaning | Default |
|---|---|---|
| `accounts` | Accounts the menu always shows, even with no profile. | none |
| `claude_path` | The real `claude` binary. | first `claude` on `PATH` that isn't this program |
| `tmp_dir` | Claude's temp directory (`CLAUDE_CODE_TMPDIR`, `TMPDIR`). Must be on disk, and `<tmp_dir>/claude-<uid>` at most 44 bytes. | `~/.claude-tmp` |
| `link_dir` | Where `setup install` puts one symlink per config. | `~/bin` |
| `ignore_profiles` | Discovered profiles to hide. | none |
| `extra_profiles` | Profiles outside `~/.claude-*`: name → directory. | none |
| `session_limits` | Limits for every session's scope. | `MemoryHigh=6G`, `MemoryMax=8G`, `MemorySwapMax=2G`, `TasksMax=4096` |
| `profiles.<name>.account` | The account this profile should be logged into; a mismatch is a warning. | none |
| `profiles.<name>.slice_limits` | Limits for all of the profile's sessions together. | none |
| `profiles.<name>.session_limits` | Per-session overrides for this profile. | none |
| `configs.<name>.profile` | The profile the config launches in. | required |
| `configs.<name>.args` | Arguments placed before the user's. | none |
| `configs.<name>.env` / `env_default` | Variables always set / set only when unset. | none |
| `configs.<name>.path_prepend` | Directories put at the front of `PATH`. | none |
| `configs.<name>.require_env` | Variables that must be set, or the launch fails. | none |
| `configs.<name>.name_arg` | `{"env", "prefix"}`: when `env` is unset, the first argument is NAME, `env=prefix+NAME`, and `--name prefix+NAME` is added. | none |
| `configs.<name>.session_limits` | Per-session overrides for this config. | none |

Limits accept `CPUQuota`, `CPUWeight`, `MemoryHigh`, `MemoryMax`,
`MemorySwapMax` and `TasksMax`, with systemd's syntax.

## How it works

- **cgroups.** A launch replaces itself with
  `systemd-run --user --scope --slice=claude-<profile>.slice -p … -- claude …`,
  so Claude keeps the terminal, PID and exit status. The hierarchy is
  `claude.slice` → `claude-<profile>.slice` → one scope per session.
  `setup install` (and every launch) keeps the slice unit files in
  `~/.config/systemd/user/` current; files without the "Managed by
  claude-profile" first line are never touched. A launch from inside a
  session of the same profile does not nest another scope.
  `CLAUDE_PROFILE_NO_CGROUP=1` skips the scope, with a warning.
- **Temp files.** `CLAUDE_CODE_TMPDIR` and `TMPDIR` point at `tmp_dir`.
  Programs that hard-code `/tmp` are not stopped, but tmpfs pages count
  against the session's memory limit, so they cannot grow without bound. A
  user `tmpfiles.d` rule removes entries untouched for 14 days.
- **Session names.** Claude Code records a session's name in its transcript
  (`custom-title` and `agent-name` records, repeated every turn). The lookup
  reads the registry of running sessions, then the last 256 KiB of each
  transcript; a session whose newest name record is further back is not
  found by name, and the menu is shown instead.

## For other tools

- Every wrapped Claude has `CLAUDE_PROFILE=<name>` in its environment.
- `claude-profile which --json <uuid|name|pid>` prints a list of
  `{"profile", "config_dir", "account", "session_id", "transcript", "pid"}`
  (empty fields omitted).
- `claude-profile list --json` prints
  `{"accounts": [...], "profiles": [{"name", "dir", "default", "account", "expected_account", "logged_in", "note"}], "configs": [{"name", "profile"}]}`.
- `claude-profile run --profile <p> -- --resume <id>` resumes in a known
  profile.

## Development

```
make test        # unit tests; temp files go to ./tmp, never /tmp
make test-live   # also starts real systemd user scopes
make vet fmt-check
```

Licence: Apache-2.0.
````

- [ ] **Step 5: Check the README's commands match the code**

Run: `make build && ./claude-profile help && rm claude-profile`
Expected: the help lists `doctor`, `list`, `new`, `run`, `setup`, `which`, `help` and `version`, matching the Usage table.

- [ ] **Step 6: Commit**

```bash
git add nfpm.yaml .github packaging README.md
git commit -m "Package as a Debian package with an apt repository

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

---

### Task 14: Create and configure the GitHub repository

This task is outward-facing. Run it only after Tasks 1–13 are committed and `make test`, `make test-live`, `make vet` and `make fmt-check` all pass.

- [ ] **Step 1: Load the repository-setup skill**

Invoke the `github-repo-setup:github-setup` skill and follow it, with these values taken from `go-tmux-saver` and `go-claude-teleport` (verified with `gh` on 2026-11-18):

- Public; description "Launch Claude Code in per-account profiles: account choice, resume lookup, cgroup limits, on-disk temp files"; no topics; default branch `main`.
- Merge commits only (`squash` and `rebase` off), delete branch on merge, allow update branch, no auto-merge, `merge_commit_title=MERGE_MESSAGE`, `merge_commit_message=PR_TITLE`.
- Issues on; wiki, projects, discussions off.
- Secret scanning and push protection on.
- Actions: all actions allowed, default workflow permissions `read`, cannot approve PRs.
- Branch protection on `main`: no required checks or reviews, force pushes and deletions disallowed.
- Tag ruleset "Enforce vXX.ZZZ version tags": target tag, active, no bypass, rule `creation`, include `refs/tags/*`, exclude `refs/tags/v[0-9].[0-9]`, `v[0-9].[0-9][0-9]`, `v[0-9].[0-9][0-9][0-9]`, `v[0-9][0-9].[0-9]`, `v[0-9][0-9].[0-9][0-9]`, `v[0-9][0-9].[0-9][0-9][0-9]` and `refs/tags/build-*`.

- [ ] **Step 2: Create the repository and push `main`**

```bash
cd ~/github/mithro/go-claude-profile
gh repo create mithro/go-claude-profile --public --description "Launch Claude Code in per-account profiles: account choice, resume lookup, cgroup limits, on-disk temp files"
git remote add origin git@github.com:mithro/go-claude-profile.git
ssh -T git@github.com   # prints "Hi mithro!" and exits 1: expected
git push -u origin main
```

Then apply the settings from Step 1.

- [ ] **Step 3: Create the apt signing key and secret**

The key is made in a keyring inside the worktree's `tmp/` (never `/tmp`) and deleted afterwards:

```bash
cd ~/github/mithro/go-claude-profile/.worktrees/initial
mkdir -m 0700 -p tmp/gnupg
export GNUPGHOME=$PWD/tmp/gnupg
gpg --batch --passphrase '' --quick-gen-key 'go-claude-profile apt repository <me@mith.ro>' rsa4096 sign never
gpg --fingerprint 'go-claude-profile apt repository'
gpg --export-secret-keys --armor 'go-claude-profile apt repository' | gh secret set APT_GPG_PRIVATE_KEY --repo mithro/go-claude-profile
gh secret list --repo mithro/go-claude-profile
gpgconf --kill gpg-agent
rm -r tmp/gnupg
unset GNUPGHOME
```

Expected: `gh secret list` shows `APT_GPG_PRIVATE_KEY`. Record the fingerprint printed by `gpg --fingerprint`.

- [ ] **Step 4: Add the fingerprint to the README**

In `README.md`, after the sentence about the suites, add (with the real fingerprint, in gpg's spaced form):

```markdown
The repository's signing key has the fingerprint
`XXXX XXXX XXXX XXXX XXXX  XXXX XXXX XXXX XXXX XXXX`.
```

Commit:

```bash
git add README.md
git commit -m "Give the apt signing key's fingerprint

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF"
```

- [ ] **Step 5: Enable Pages and limit the environment to `main`**

```bash
gh api -X POST repos/mithro/go-claude-profile/pages -f build_type=workflow
gh api -X PUT repos/mithro/go-claude-profile/environments/github-pages \
  -F 'deployment_branch_policy[protected_branches]=false' -F 'deployment_branch_policy[custom_branch_policies]=true'
gh api -X POST repos/mithro/go-claude-profile/environments/github-pages/deployment-branch-policies -f name=main
gh api repos/mithro/go-claude-profile/pages --jq '{build_type, html_url, https_enforced}'
```

Expected: `build_type` `workflow`, `html_url` `https://mith.ro/go-claude-profile/`.

- [ ] **Step 6: Open the pull request and check CI**

```bash
git push -u origin initial
gh pr create --repo mithro/go-claude-profile --base main --head initial \
  --title "claude-profile: first version" --body-file - <<'EOF'
First version of claude-profile, implementing docs/superpowers/specs/2026-11-19-claude-profile-design.md
by the plan in docs/superpowers/plans/2026-11-19-claude-profile.md.

- Profiles discovered from ~/.claude and ~/.claude-*, grouped by the account each is logged into
- --resume <uuid|name> launches in the profile that owns the session
- Configs as named launch presets; a symlink named after a config is its launcher
- Each session runs in a systemd user scope under claude.slice/claude-<profile>.slice
- CLAUDE_CODE_TMPDIR and TMPDIR on disk (~/.claude-tmp)
- setup, doctor, list and which
- nfpm Debian packages and the signed apt repository at https://mith.ro/go-claude-profile/

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01T8XQLw8BFhTVxZViKam9RF
EOF
gh pr checks --repo mithro/go-claude-profile --watch
```

Expected: `test` and every `build-deb (...)` job pass; `publish-apt` and `release` are skipped on a pull request. If a job fails, use the `superpowers:systematic-debugging` skill on its log (`gh run view --log-failed`), fix on the branch, commit, push.

- [ ] **Step 7: Stop for Tim**

Report the PR URL and the check results. **Do not merge.** Merging publishes the apt repository and the first GitHub Release; Tim decides when, and whether to make a `v0.1` tag first (without one, versions are `0.0.post<N>`).

---

### Task 15: Deploy on desktop (only when Tim says so)

Do this only after Tim has merged the PR and asked for the deployment. Every step changes his live setup; confirm each one.

- [ ] **Step 1: Install from the apt repository** — the commands in the README's Install section (they need `sudo`; Tim may prefer to run them himself with `! <command>`). Check: `claude-profile version`.
- [ ] **Step 2: Write `~/.config/claude-profile/config.json`** with the three accounts in `accounts`, `ignore_profiles: ["old"]`, and configs reproducing `claude-ac`, `claude-ac-coordinator`, `claude-bl` and `claude-bl-coordinator` exactly as their scripts in `~/src/west/scratch/bin/` and `~/src/beta/scratch/bin/` do today (show Tim the file before writing it). Leave the expected `account` for each profile for Tim to fill in: today `~/.claude-west-corp` and `~/.claude-beta-lab` are both logged into `claude@example.com`.
- [ ] **Step 3:** `claude-profile setup install`, then `claude-profile doctor`; report the output verbatim.
- [ ] **Step 4:** `claude-profile list` and `claude-profile which <a live session's pid>`; report.
- [ ] **Step 5:** Do **not** replace the scratch-repo `claude-ac*` / `claude-bl*` scripts or change `go-tmux-saver`, `claude-resume` or the backup; that is the follow-up spec.
