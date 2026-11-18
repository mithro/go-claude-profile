package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type execCall struct {
	path      string
	argv, env []string
}

type fixture struct {
	t     *testing.T
	root  string
	home  string
	bin   string // on PATH; holds a fake claude
	d     Deps
	out   *bytes.Buffer
	errb  *bytes.Buffer
	execs []execCall
	runs  [][]string
	tty   string // menu answers; "" means there is no terminal
}

var errExecRecorded = errors.New("exec recorded")

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	f.write(filepath.Join(f.bin, "claude"), "#!/bin/sh\nexit 0\n", 0o755)
	f.write(filepath.Join(root, "claude-profile"), "#!/bin/sh\nexit 0\n", 0o755)
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	f.d = Deps{
		Env: map[string]string{
			"HOME":            f.home,
			"PATH":            f.bin,
			"XDG_CONFIG_HOME": filepath.Join(f.home, ".config"),
			"XDG_STATE_HOME":  filepath.Join(f.home, ".local", "state"),
		},
		UID:        1000,
		Stdin:      strings.NewReader(""),
		Stdout:     f.out,
		Stderr:     f.errb,
		Self:       filepath.Join(root, "claude-profile"),
		ProcRoot:   filepath.Join(root, "proc"),
		CgroupRoot: filepath.Join(root, "cgroup"),
		Exec: func(path string, argv, env []string) error {
			f.execs = append(f.execs, execCall{path, argv, env})
			return errExecRecorded
		},
		Run: func(argv, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
			f.runs = append(f.runs, argv)
			return nil
		},
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		OpenTTY: func() (io.ReadCloser, error) {
			if f.tty == "" {
				return nil, errors.New("no terminal")
			}
			return io.NopCloser(strings.NewReader(f.tty)), nil
		},
		CheckTmp: func(string, int) error { return nil },
	}
	return f
}

func (f *fixture) write(path, body string, mode fs.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		f.t.Fatal(err)
	}
}

// profile creates a logged-in profile ("" email: logged out).
func (f *fixture) profile(name, email string) string {
	f.t.Helper()
	body := `{}`
	if email != "" {
		body = `{"oauthAccount": {"emailAddress": "` + email + `"}}`
	}
	if name == "default" {
		dir := filepath.Join(f.home, ".claude")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			f.t.Fatal(err)
		}
		f.write(filepath.Join(f.home, ".claude.json"), body, 0o644)
		return dir
	}
	dir := filepath.Join(f.home, ".claude-"+name)
	f.write(filepath.Join(dir, ".claude.json"), body, 0o644)
	return dir
}

func (f *fixture) config(body string) {
	f.t.Helper()
	f.write(filepath.Join(f.home, ".config", "claude-profile", "config.json"), body, 0o644)
}

func (f *fixture) run(argv0 string, args ...string) int {
	f.t.Helper()
	f.out.Reset()
	f.errb.Reset()
	return RunMultiCall(argv0, args, f.d)
}
