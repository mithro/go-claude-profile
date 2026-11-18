package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mithro/go-claude-profile/internal/managed"
)

func TestTmpfiles(t *testing.T) {
	if got := TmpfilesDir(map[string]string{}, "/h"); got != "/h/.config/user-tmpfiles.d" {
		t.Errorf("TmpfilesDir = %q", got)
	}
	got := TmpfilesContent("/h/.claude-tmp")
	if got != managed.Marker+"\nd /h/.claude-tmp 0700 - - 14d\n" {
		t.Errorf("TmpfilesContent = %q", got)
	}
}

func TestLinks(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "claude-profile")
	if err := os.WriteFile(self, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "claude-mine"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink(self, filepath.Join(dir, "claude-ok"))
	names := []string{"claude-new", "claude-ok", "claude-mine"}

	dry, err := SyncLinks(dir, self, names, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "claude-new")); err == nil {
		t.Error("dry run created a link")
	}
	res, err := SyncLinks(dir, self, names, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dry, res) {
		t.Errorf("dry %+v != real %+v", dry, res)
	}
	j := func(n string) string { return filepath.Join(dir, n) }
	want := LinkResult{Created: []string{j("claude-new")}, Unchanged: []string{j("claude-ok")}, Foreign: []string{j("claude-mine")}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("SyncLinks = %+v, want %+v", res, want)
	}
	if target, _ := os.Readlink(j("claude-new")); target != self {
		t.Errorf("claude-new -> %q", target)
	}
	removed, err := RemoveLinks(dir, self, names)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{j("claude-new"), j("claude-ok")}) {
		t.Errorf("RemoveLinks = %q", removed)
	}
	if data, _ := os.ReadFile(j("claude-mine")); !strings.HasPrefix(string(data), "#!") {
		t.Error("RemoveLinks touched a file it does not own")
	}
}
