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
