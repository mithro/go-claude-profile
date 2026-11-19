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
