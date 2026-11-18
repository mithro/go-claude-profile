package cli

import (
	"encoding/json"
	"reflect"
	"slices"
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

// keys returns the sorted key set of a decoded JSON object.
func keys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func TestListJSONKeys(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.profile("work", "c@example.com")
	f.config(`{"profiles": {"work": {"account": "w@example.com"}}, "configs": {"claude-w": {"profile": "work"}}}`)
	if got := f.run("claude-profile", "list", "--json"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var out map[string]any
	if err := json.Unmarshal(f.out.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got, want := keys(out), []string{"accounts", "configs", "profiles"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %q, want %q", got, want)
	}
	profs := out["profiles"].([]any)
	base := []string{"account", "default", "dir", "logged_in", "name"}
	if got := keys(profs[0].(map[string]any)); !reflect.DeepEqual(got, base) {
		t.Errorf("default profile keys = %q, want %q", got, base)
	}
	withExpected := append(slices.Clone(base), "expected_account")
	slices.Sort(withExpected)
	if got := keys(profs[1].(map[string]any)); !reflect.DeepEqual(got, withExpected) {
		t.Errorf("work profile keys = %q, want %q", got, withExpected)
	}
	cfg := out["configs"].([]any)[0].(map[string]any)
	if got, want := keys(cfg), []string{"name", "profile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("config keys = %q, want %q", got, want)
	}
}

func TestListJSONNoteKey(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	f.write(f.home+"/.claude-broken/.claude.json", "{not json", 0o644)
	if got := f.run("claude-profile", "list", "--json"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var out struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(f.out.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Profiles {
		if p["name"] == "broken" {
			if _, ok := p["note"]; !ok {
				t.Errorf("broken profile has no note key: %v", p)
			}
			return
		}
	}
	t.Errorf("no broken profile in %s", f.out)
}
