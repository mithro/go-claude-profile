package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "absent.json"), "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if f.TmpDir != "/home/u/.claude-tmp" {
		t.Errorf("TmpDir = %q", f.TmpDir)
	}
	if f.LinkDir != "/home/u/bin" {
		t.Errorf("LinkDir = %q", f.LinkDir)
	}
	if !reflect.DeepEqual(f.SessionLimits, DefaultSessionLimits()) {
		t.Errorf("SessionLimits = %v", f.SessionLimits)
	}
}

func TestLoadExpandsAndMerges(t *testing.T) {
	path := writeConfig(t, `{
		"tmp_dir": "~/scratch-tmp",
		"session_limits": {"MemoryMax": "4G"},
		"extra_profiles": {"work": "~/elsewhere/claude"},
		"profiles": {"work": {"account": "a@example.com",
			"slice_limits": {"MemoryMax": "20G"}, "session_limits": {"TasksMax": "100"}}},
		"configs": {"claude-w": {"profile": "work", "args": ["--model", "opus"],
			"path_prepend": ["~/bin/wrappers"], "session_limits": {"CPUQuota": "200%"}}}
	}`)
	f, err := Load(path, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if f.TmpDir != "/home/u/scratch-tmp" {
		t.Errorf("TmpDir = %q", f.TmpDir)
	}
	if got := f.ExtraProfiles["work"]; got != "/home/u/elsewhere/claude" {
		t.Errorf("ExtraProfiles[work] = %q", got)
	}
	if got := f.Configs["claude-w"].PathPrepend; !reflect.DeepEqual(got, []string{"/home/u/bin/wrappers"}) {
		t.Errorf("PathPrepend = %q", got)
	}
	want := Limits{"MemoryHigh": "6G", "MemoryMax": "4G", "MemorySwapMax": "2G", "TasksMax": "4096"}
	if !reflect.DeepEqual(f.SessionLimits, want) {
		t.Errorf("SessionLimits = %v, want %v", f.SessionLimits, want)
	}
	got := f.SessionLimitsFor("work", "claude-w")
	want = Limits{"MemoryHigh": "6G", "MemoryMax": "4G", "MemorySwapMax": "2G", "TasksMax": "100", "CPUQuota": "200%"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionLimitsFor(work, claude-w) = %v, want %v", got, want)
	}
	if got := f.SessionLimitsFor("other", ""); !reflect.DeepEqual(got, f.SessionLimits) {
		t.Errorf("SessionLimitsFor(other) = %v", got)
	}
	if got := f.Profiles["work"].SliceLimits["MemoryMax"]; got != "20G" {
		t.Errorf("slice MemoryMax = %q", got)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"unknown key":       {`{"acounts": []}`, `unknown field "acounts"`},
		"limit name":        {`{"session_limits": {"MemoryLimit": "1G"}}`, `"MemoryLimit" is not one of`},
		"limit value":       {`{"session_limits": {"MemoryMax": "1 G"}}`, "invalid value"},
		"profile name":      {`{"profiles": {"Work": {}}}`, `invalid profile name "Work"`},
		"reserved name":     {`{"profiles": {"tmp": {}}}`, `"tmp" is reserved`},
		"extra default":     {`{"extra_profiles": {"default": "/x"}}`, "cannot be redefined"},
		"config name":       {`{"configs": {"claude-profile": {"profile": "default"}}}`, "invalid config name"},
		"config no profile": {`{"configs": {"c": {}}}`, "profile is required"},
		"name_arg env":      {`{"configs": {"c": {"profile": "default", "name_arg": {"prefix": "x-"}}}}`, "name_arg.env is required"},
		"env name":          {`{"configs": {"c": {"profile": "default", "env": {"A=B": "x"}}}}`, "invalid environment variable name"},
		"slice limit":       {`{"profiles": {"w": {"slice_limits": {"Nice": "5"}}}}`, `"Nice" is not one of`},
		"bad json":          {`{`, "unexpected EOF"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.body), "/home/u")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestPath(t *testing.T) {
	if got := Path(map[string]string{}, "/home/u"); got != "/home/u/.config/claude-profile/config.json" {
		t.Errorf("default Path = %q", got)
	}
	if got := Path(map[string]string{"XDG_CONFIG_HOME": "/x"}, "/home/u"); got != "/x/claude-profile/config.json" {
		t.Errorf("XDG Path = %q", got)
	}
}

func TestConfigNamesSorted(t *testing.T) {
	f := &File{Configs: map[string]Config{"b": {}, "a": {}, "c": {}}}
	if got := f.ConfigNames(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("ConfigNames = %q", got)
	}
}

func TestExpand(t *testing.T) {
	for in, want := range map[string]string{"~": "/h", "~/a/b": "/h/a/b", "/abs": "/abs", "rel": "rel", "~x": "~x"} {
		if got := Expand("/h", in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}
