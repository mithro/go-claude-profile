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
