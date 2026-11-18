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
