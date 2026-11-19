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
		"default":   "claude-default.slice",
		"west-corp": `claude-west\x2dcorp.slice`,
		"fo2":       "claude-fo2.slice",
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
