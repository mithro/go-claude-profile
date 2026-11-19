package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/resume"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"which", "show the profile owning a session: which [--json] UUID|NAME|PID", cmdWhich})
}

type whichRow struct {
	Profile    string `json:"profile"`
	ConfigDir  string `json:"config_dir"`
	Account    string `json:"account"`
	SessionID  string `json:"session_id,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	PID        int    `json:"pid,omitempty"`
}

func cmdWhich(args []string, d Deps) int {
	fs := flag.NewFlagSet("which", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return fail(d, usage.Errorf("usage: %s which [--json] SESSION-UUID|SESSION-NAME|PID", toolName))
	}
	arg := fs.Arg(0)
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	var rows []whichRow
	if pid, err := strconv.Atoi(arg); err == nil && pid > 0 {
		row, err := whichPID(d, w, pid)
		if err != nil {
			return fail(d, err)
		}
		rows = []whichRow{row}
	} else {
		hits, err := resume.Lookup(w.profiles, arg)
		if err != nil {
			return fail(d, err)
		}
		if len(hits) == 0 {
			return fail(d, fmt.Errorf("no session %q in any profile", arg))
		}
		for _, h := range hits {
			p, _ := profile.Find(w.profiles, h.Profile)
			rows = append(rows, whichRow{Profile: p.Name, ConfigDir: p.Dir, Account: p.Account, SessionID: h.SessionID, Transcript: h.Transcript})
		}
	}
	if *asJSON {
		return writeJSON(d, rows)
	}
	for _, r := range rows {
		fmt.Fprintf(d.Stdout, "%s\t%s\t%s\t%s\n", dash(r.Profile), dash(r.Account), dash(r.SessionID), r.ConfigDir)
	}
	return exitOK
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// whichPID reads a process's CLAUDE_CONFIG_DIR to find its profile, and the
// profile's session registry for its session ID.
func whichPID(d Deps, w *world, pid int) (whichRow, error) {
	data, err := os.ReadFile(filepath.Join(d.ProcRoot, strconv.Itoa(pid), "environ"))
	if err != nil {
		return whichRow{}, err
	}
	var dir string
	for _, kv := range bytes.Split(data, []byte{0}) {
		if v, ok := bytes.CutPrefix(kv, []byte("CLAUDE_CONFIG_DIR=")); ok {
			dir = string(v)
		}
	}
	var p profile.Profile
	var ok bool
	if dir == "" {
		p, ok = profile.Find(w.profiles, "default")
		dir = filepath.Join(w.home, ".claude")
	} else {
		p, ok = profile.ByDir(w.profiles, dir)
	}
	if !ok {
		return whichRow{ConfigDir: dir, PID: pid}, nil
	}
	row := whichRow{Profile: p.Name, ConfigDir: p.Dir, Account: p.Account, PID: pid}
	if data, err := os.ReadFile(filepath.Join(p.Dir, "sessions", strconv.Itoa(pid)+".json")); err == nil {
		var e struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(data, &e) == nil {
			row.SessionID = e.SessionID
		}
	}
	return row, nil
}
