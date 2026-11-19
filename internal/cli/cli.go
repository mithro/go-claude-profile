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
