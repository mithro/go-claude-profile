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
		{"", "profile:default", "cancel"}, // EOF must not pick the default
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
