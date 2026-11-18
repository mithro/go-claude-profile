package cli

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		d, out, _ := testDeps(t)
		if got := RunMultiCall("/usr/bin/claude-profile", []string{arg}, d); got != exitOK {
			t.Fatalf("%s: exit %d", arg, got)
		}
		if got, want := out.String(), "claude-profile dev\n"; got != want {
			t.Errorf("%s: got %q, want %q", arg, got, want)
		}
	}
}

func TestNoArgumentsIsUsage(t *testing.T) {
	d, _, errb := testDeps(t)
	if got := RunMultiCall("claude-profile", nil, d); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if !strings.Contains(errb.String(), "usage: claude-profile") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	d, _, errb := testDeps(t)
	if got := RunMultiCall("claude-profile", []string{"frob"}, d); got != exitUsage {
		t.Fatalf("exit %d, want %d", got, exitUsage)
	}
	if want := `claude-profile: unknown command "frob"`; !strings.Contains(errb.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errb.String(), want)
	}
}
