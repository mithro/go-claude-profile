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
