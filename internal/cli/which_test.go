package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const sid = "11111111-2222-3333-4444-555555555555"

func TestWhichSession(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(work, "projects", "-src", sid+".jsonl"), "{}\n", 0o644)
	if got := f.run("claude-profile", "which", "--json", sid); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	var rows []whichRow
	if err := json.Unmarshal(f.out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Profile != "work" || rows[0].Account != "c@example.com" || rows[0].SessionID != sid || rows[0].ConfigDir != work {
		t.Errorf("rows = %+v", rows)
	}
	if got := f.run("claude-profile", "which", "no-such-session"); got != exitFail {
		t.Errorf("unknown session: exit %d", got)
	}
}

func TestWhichPID(t *testing.T) {
	f := newFixture(t)
	f.profile("default", "me@example.com")
	work := f.profile("work", "c@example.com")
	f.write(filepath.Join(f.root, "proc", "4242", "environ"), "HOME=/h\x00CLAUDE_CONFIG_DIR="+work+"\x00", 0o644)
	f.write(filepath.Join(work, "sessions", "4242.json"), `{"pid":4242,"sessionId":"`+sid+`"}`, 0o644)
	f.write(filepath.Join(f.root, "proc", "77", "environ"), "HOME=/h\x00", 0o644)

	if got := f.run("claude-profile", "which", "4242"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if want := "work\tc@example.com\t" + sid + "\t" + work + "\n"; f.out.String() != want {
		t.Errorf("which 4242 = %q, want %q", f.out.String(), want)
	}
	if got := f.run("claude-profile", "which", "77"); got != exitOK {
		t.Fatalf("exit %d: %s", got, f.errb)
	}
	if !strings.HasPrefix(f.out.String(), "default\tme@example.com\t-\t") {
		t.Errorf("which 77 = %q", f.out.String())
	}
}
