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
		if r, err := filepath.EvalSymlinks(configured); err == nil && r == self {
			return "", usage.Errorf("claude_path %s is claude-profile itself", configured)
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
