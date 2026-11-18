package cli

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	f := newFixture(t)
	for _, arg := range []string{"version", "--version", "-v"} {
		if got := f.run("/usr/bin/claude-profile", arg); got != exitOK {
			t.Fatalf("%s: exit %d", arg, got)
		}
		if got, want := f.out.String(), "claude-profile dev\n"; got != want {
			t.Errorf("%s: got %q, want %q", arg, got, want)
		}
	}
}

func TestNoArgumentsIsUsage(t *testing.T) {
	f := newFixture(t)
	if got := f.run("claude-profile"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(f.errb.String(), "usage: claude-profile") {
		t.Errorf("stderr = %q", f.errb.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	f := newFixture(t)
	if got := f.run("claude-profile", "frob"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if want := `claude-profile: unknown command "frob"`; !strings.Contains(f.errb.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", f.errb.String(), want)
	}
}

func TestBrokenConfigStopsCommands(t *testing.T) {
	f := newFixture(t)
	f.config(`{"nonsense": 1}`)
	if got := f.run("claude-profile", "list"); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(f.errb.String(), `unknown field "nonsense"`) {
		t.Errorf("stderr = %q", f.errb.String())
	}
	if got := f.run("claude-profile", "version"); got != exitOK {
		t.Errorf("version with a broken config: exit %d", got)
	}
}
