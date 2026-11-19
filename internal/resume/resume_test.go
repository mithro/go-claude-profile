package resume

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/profile"
)

const (
	idA = "11111111-2222-3333-4444-555555555555"
	idB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
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

func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func title(name, id string) string {
	return `{"type":"custom-title","customTitle":"` + name + `","sessionId":"` + id + `"}`
}

func agent(name, id string) string {
	return `{"type":"agent-name","agentName":"` + name + `","sessionId":"` + id + `"}`
}

func padding(n int) string { return `{"type":"user","message":"` + strings.Repeat("x", n) + `"}` }

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestValue(t *testing.T) {
	cases := []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "", false},
		{[]string{"--resume", "abc"}, "abc", true},
		{[]string{"-r", "abc"}, "abc", true},
		{[]string{"--resume=abc"}, "abc", true},
		{[]string{"--model", "opus", "--resume", "ac-issues", "hello"}, "ac-issues", true},
		{[]string{"--resume"}, "", false},
		{[]string{"--resume", "--model", "opus"}, "", false},
		{[]string{"--resume="}, "", false},
		{[]string{"--continue"}, "", false},
		{[]string{"--", "--resume", "abc"}, "", false},
	}
	for _, tc := range cases {
		got, ok := Value(tc.args)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Value(%q) = %q, %v; want %q, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !IsUUID(idA) || IsUUID("ac-issues") || IsUUID(strings.ToUpper(idB)) {
		t.Error("IsUUID gave a wrong answer")
	}
}

// The default profile's projects/ holds a symlink into the work profile's
// projects/, as adopt_projects.py leaves them: the hit belongs to work, once.
func TestLookupUUIDThroughBackSymlink(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".claude-work")
	def := filepath.Join(home, ".claude")
	mkfile(t, filepath.Join(work, "projects", "-src-a", idA+".jsonl"), "{}\n")
	if err := os.MkdirAll(filepath.Join(def, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "projects", "-src-a"), filepath.Join(def, "projects", "-src-a")); err != nil {
		t.Fatal(err)
	}
	ps := []profile.Profile{{Name: "default", Dir: def, Default: true}, {Name: "work", Dir: work}}

	got, err := Lookup(ps, idA)
	if err != nil {
		t.Fatal(err)
	}
	want := []Hit{{Profile: "work", SessionID: idA, Transcript: resolved(t, filepath.Join(work, "projects", "-src-a", idA+".jsonl"))}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lookup:\n got %+v\nwant %+v", got, want)
	}
}

func TestLookupUUIDInTwoProfiles(t *testing.T) {
	home := t.TempDir()
	ps := []profile.Profile{{Name: "a", Dir: filepath.Join(home, ".claude-a")}, {Name: "b", Dir: filepath.Join(home, ".claude-b")}}
	for _, p := range ps {
		mkfile(t, filepath.Join(p.Dir, "projects", "-x", idA+".jsonl"), "{}\n")
	}
	got, err := Lookup(ps, idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Profile != "a" || got[1].Profile != "b" {
		t.Errorf("Lookup = %+v, want hits in a and b", got)
	}
}

func TestLookupName(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".claude-work")
	def := filepath.Join(home, ".claude")
	ps := []profile.Profile{{Name: "default", Dir: def, Default: true}, {Name: "work", Dir: work}}
	// A running session, found through the registry.
	mkfile(t, filepath.Join(work, "sessions", "4242.json"), `{"pid":4242,"sessionId":"`+idA+`","name":"ac-issues"}`)
	// A renamed session: only its last name counts.
	renamed := filepath.Join(def, "projects", "-x", idB+".jsonl")
	mkfile(t, renamed, lines(title("old-name", idB), padding(100), title("new-name", idB), padding(100)))
	// A session with only an agent name, in a file larger than the tail.
	agented := filepath.Join(work, "projects", "-y", idA+".jsonl")
	mkfile(t, agented, lines(padding(300<<10), agent("bl-coordinator", idA)))

	cases := map[string][]Hit{
		"ac-issues":      {{Profile: "work", SessionID: idA}},
		"new-name":       {{Profile: "default", SessionID: idB, Transcript: resolved(t, renamed)}},
		"old-name":       nil,
		"bl-coordinator": {{Profile: "work", SessionID: idA, Transcript: resolved(t, agented)}},
		"nobody":         nil,
	}
	for name, want := range cases {
		got, err := Lookup(ps, name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Lookup(%q):\n got %+v\nwant %+v", name, got, want)
		}
	}
}

func TestNamesReadsOnlyTheTail(t *testing.T) {
	dir := t.TempDir()
	early := filepath.Join(dir, "early.jsonl")
	mkfile(t, early, lines(title("early", idA), padding(300<<10)))
	if got, _ := Names(early); got != "" {
		t.Errorf("Names(early) = %q, want \"\" (name is before the last 256 KiB)", got)
	}
	late := filepath.Join(dir, "late.jsonl")
	mkfile(t, late, lines(padding(300<<10), title("late", idA)))
	if got, _ := Names(late); got != "late" {
		t.Errorf("Names(late) = %q, want %q", got, "late")
	}
	if got, ag := Names(filepath.Join(dir, "missing.jsonl")); got != "" || ag != "" {
		t.Errorf("Names(missing) = %q, %q", got, ag)
	}
}
