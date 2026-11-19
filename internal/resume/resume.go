// Package resume finds which profile owns the session a --resume argument
// names, by session UUID or by session name.
package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mithro/go-claude-profile/internal/profile"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID reports whether s looks like a Claude Code session ID.
func IsUUID(s string) bool { return uuidRE.MatchString(s) }

// Value returns the session named by --resume v, --resume=v or -r v. A
// --resume with no value (Claude's own picker) names no session.
func Value(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return "", false
		}
		if v, ok := strings.CutPrefix(a, "--resume="); ok {
			if v != "" {
				return v, true
			}
			continue
		}
		if a == "--resume" || a == "-r" {
			if i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1], true
			}
			return "", false
		}
	}
	return "", false
}

// Hit is one session that matches.
type Hit struct {
	Profile    string // the profile that owns the session
	SessionID  string
	Transcript string // symlink-resolved path; "" when found only in the registry
}

// Lookup finds the sessions value names in any profile. A transcript reached
// through a symlink (such as the back-symlinks under ~/.claude/projects)
// belongs to the profile whose directory really holds it, and counts once.
func Lookup(profiles []profile.Profile, value string) ([]Hit, error) {
	l := newLocator(profiles)
	if IsUUID(value) {
		for _, p := range profiles {
			matches, err := filepath.Glob(filepath.Join(p.Dir, "projects", "*", value+".jsonl"))
			if err != nil {
				return nil, err
			}
			for _, m := range matches {
				l.transcript(p.Name, m, value)
			}
		}
		return l.result(), nil
	}
	for _, p := range profiles {
		l.registry(p, value)
	}
	for _, p := range profiles {
		matches, err := filepath.Glob(filepath.Join(p.Dir, "projects", "*", "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			r, err := filepath.EvalSymlinks(m)
			if err != nil || l.seen[r] {
				continue
			}
			l.seen[r] = true
			if title, agent := Names(r); title == value || agent == value {
				id := strings.TrimSuffix(filepath.Base(r), ".jsonl")
				l.add(Hit{Profile: l.ownerOf(r, p.Name), SessionID: id, Transcript: r})
			}
		}
	}
	return l.result(), nil
}

// tailBytes is how much of each transcript Names reads. Claude Code repeats
// the name records with every turn, so the newest one is near the end.
const tailBytes = 256 << 10

// Names returns the last custom-title and agent-name recorded in the last
// 256 KiB of a transcript.
func Names(path string) (title, agent string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", ""
	}
	off := max(st.Size()-tailBytes, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return "", ""
	}
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		switch {
		case bytes.Contains(line, []byte(`"type":"custom-title"`)):
			var r struct {
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(line, &r) == nil && r.CustomTitle != "" {
				title = r.CustomTitle
			}
		case bytes.Contains(line, []byte(`"type":"agent-name"`)):
			var r struct {
				AgentName string `json:"agentName"`
			}
			if json.Unmarshal(line, &r) == nil && r.AgentName != "" {
				agent = r.AgentName
			}
		}
	}
	return title, agent
}

type owner struct{ name, dir string }

type locator struct {
	owners []owner           // symlink-resolved profile directories, longest first
	seen   map[string]bool   // resolved transcript paths already read
	hits   map[[2]string]Hit // keyed by profile and session ID
}

func newLocator(ps []profile.Profile) *locator {
	l := &locator{seen: map[string]bool{}, hits: map[[2]string]Hit{}}
	for _, p := range ps {
		if r, err := filepath.EvalSymlinks(p.Dir); err == nil {
			l.owners = append(l.owners, owner{p.Name, r})
		}
	}
	sort.Slice(l.owners, func(i, j int) bool { return len(l.owners[i].dir) > len(l.owners[j].dir) })
	return l
}

func (l *locator) ownerOf(realPath, fallback string) string {
	for _, o := range l.owners {
		if strings.HasPrefix(realPath, o.dir+string(filepath.Separator)) {
			return o.name
		}
	}
	return fallback
}

func (l *locator) transcript(profileName, path, id string) {
	r, err := filepath.EvalSymlinks(path)
	if err != nil || l.seen[r] {
		return
	}
	l.seen[r] = true
	l.add(Hit{Profile: l.ownerOf(r, profileName), SessionID: id, Transcript: r})
}

func (l *locator) registry(p profile.Profile, name string) {
	files, err := filepath.Glob(filepath.Join(p.Dir, "sessions", "*.json"))
	if err != nil {
		return
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e struct {
			SessionID string `json:"sessionId"`
			Name      string `json:"name"`
		}
		if json.Unmarshal(data, &e) == nil && e.Name == name && e.SessionID != "" {
			l.add(Hit{Profile: p.Name, SessionID: e.SessionID})
		}
	}
}

// add records h, preferring a hit that knows its transcript.
func (l *locator) add(h Hit) {
	key := [2]string{h.Profile, h.SessionID}
	if old, ok := l.hits[key]; ok && old.Transcript != "" {
		return
	}
	l.hits[key] = h
}

func (l *locator) result() []Hit {
	if len(l.hits) == 0 {
		return nil
	}
	out := make([]Hit, 0, len(l.hits))
	for _, h := range l.hits {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}
