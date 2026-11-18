package managed

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSync(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "units")
	owned := Marker + "\nnew\n"
	want := map[string]string{"a": owned, "b": owned, "c": owned, "d": owned}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b"), []byte(owned), 0o644)                // already right
	os.WriteFile(filepath.Join(dir, "c"), []byte(Marker+"\nold\n"), 0o644)     // ours, out of date
	os.WriteFile(filepath.Join(dir, "d"), []byte("# the user's own\n"), 0o644) // not ours

	dry, err := Sync(dir, want, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); err == nil {
		t.Error("dry run wrote a file")
	}
	res, err := Sync(dir, want, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dry, res) {
		t.Errorf("dry run %+v differs from real run %+v", dry, res)
	}
	j := func(n string) string { return filepath.Join(dir, n) }
	wantRes := Result{Written: []string{j("a"), j("c")}, Unchanged: []string{j("b")}, Foreign: []string{j("d")}}
	if !reflect.DeepEqual(res, wantRes) {
		t.Errorf("Sync = %+v, want %+v", res, wantRes)
	}
	if !res.Changed() {
		t.Error("Changed() = false")
	}
	if read(t, j("a")) != owned || read(t, j("c")) != owned || read(t, j("d")) != "# the user's own\n" {
		t.Error("file contents wrong after Sync")
	}
	again, err := Sync(dir, want, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed() {
		t.Errorf("second Sync wrote %v", again.Written)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude.slice"), []byte(Marker+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "claude-x.slice"), []byte("mine\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "other.slice"), []byte(Marker+"\n"), 0o644)
	got, err := Remove(dir, "claude*.slice")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{filepath.Join(dir, "claude.slice")}) {
		t.Errorf("Remove = %q", got)
	}
	for _, n := range []string{"claude-x.slice", "other.slice"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s was removed", n)
		}
	}
}
