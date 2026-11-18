package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestListJSON(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	f.profile("fresh", "")
	f.config(`{"accounts": ["w@example.com"], "profiles": {"work": {"account": "w@example.com"}},
		"configs": {"claude-w": {"profile": "work"}}}`)
	if got := f.run("claude-profile", "list", "--json"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var out listOutput
	if err := json.Unmarshal(f.out.Bytes(), &out); err != nil {
		t.Fatalf("%v in %s", err, f.out)
	}
	if want := []string{"c@example.com", "me@example.com", "w@example.com"}; !reflect.DeepEqual(out.Accounts, want) {
		t.Errorf("accounts = %q, want %q", out.Accounts, want)
	}
	var names []string
	for _, p := range out.Profiles {
		names = append(names, p.Name)
	}
	if want := []string{"default", "fresh", "work"}; !reflect.DeepEqual(names, want) {
		t.Errorf("profiles = %q, want %q", names, want)
	}
	work := out.Profiles[2]
	if work.Account != "c@example.com" || work.ExpectedAccount != "w@example.com" || !work.LoggedIn || work.Dir != f.home+"/.claude-work" {
		t.Errorf("work = %+v", work)
	}
	if out.Profiles[1].LoggedIn {
		t.Error("fresh is logged in")
	}
	if want := []configOut{{Name: "claude-w", Profile: "work"}}; !reflect.DeepEqual(out.Configs, want) {
		t.Errorf("configs = %+v", out.Configs)
	}
}

func TestListText(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	if got := f.run("claude-profile", "list"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if !strings.Contains(f.out.String(), "me@example.com\n  default ") || strings.Contains(f.out.String(), "new profile") {
		t.Errorf("list = %q", f.out.String())
	}
}
