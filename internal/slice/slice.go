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
