// Package menu is the interactive profile chooser: a numbered list on the
// terminal, grouped by account.
package menu

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/profile"
)

// Kind is what choosing an item does.
type Kind int

const (
	KindProfile Kind = iota // launch in a profile
	KindConfig              // launch with a config
	KindNew                 // create a profile, then launch in it
)

// Item is one choice.
type Item struct {
	Kind    Kind
	Profile string // KindProfile, KindConfig
	Config  string // KindConfig
	Account string // KindNew: the account to log in as ("" for any)
}

// Key identifies an item across runs, for remembering the last choice.
func (i Item) Key() string {
	switch i.Kind {
	case KindProfile:
		return "profile:" + i.Profile
	case KindConfig:
		return "config:" + i.Config
	default:
		return "new:" + i.Account
	}
}

// Entry is an item with its label.
type Entry struct {
	Label string
	Item  Item
}

// Group is the entries for one account.
type Group struct {
	Heading string
	Entries []Entry
}

// ErrCancelled is returned when the user quits the menu.
var ErrCancelled = errors.New("cancelled")

// Build groups the profiles by account: logged-in accounts (sorted), then
// config.json's pinned accounts that have no profile, then "not logged in".
func Build(profiles []profile.Profile, cfg *config.File) []Group {
	byAccount := map[string][]profile.Profile{}
	display := map[string]string{}
	var loggedOut []profile.Profile
	for _, p := range profiles {
		if p.Account == "" {
			loggedOut = append(loggedOut, p)
			continue
		}
		key := strings.ToLower(p.Account)
		byAccount[key] = append(byAccount[key], p)
		if _, ok := display[key]; !ok {
			display[key] = p.Account
		}
	}
	var groups []Group
	for _, key := range slices.Sorted(maps.Keys(byAccount)) {
		g := Group{Heading: display[key]}
		for _, p := range byAccount[key] {
			g.Entries = append(g.Entries, profileEntries(p, cfg)...)
		}
		g.Entries = append(g.Entries, newEntry(display[key]))
		groups = append(groups, g)
	}
	pinned := map[string]bool{}
	for _, acct := range cfg.Accounts {
		key := strings.ToLower(acct)
		if _, ok := byAccount[key]; ok || pinned[key] {
			continue
		}
		pinned[key] = true
		groups = append(groups, Group{Heading: acct + "  (no profile yet)", Entries: []Entry{newEntry(acct)}})
	}
	g := Group{Heading: "not logged in"}
	for _, p := range loggedOut {
		g.Entries = append(g.Entries, profileEntries(p, cfg)...)
	}
	g.Entries = append(g.Entries, newEntry(""))
	return append(groups, g)
}

func profileEntries(p profile.Profile, cfg *config.File) []Entry {
	label := fmt.Sprintf("%-16s %s", p.Name, p.Dir)
	if p.Mismatch() {
		label += "  !! expected " + p.Expected
	}
	if p.Note != "" {
		label += "  (" + p.Note + ")"
	}
	entries := []Entry{{Label: label, Item: Item{Kind: KindProfile, Profile: p.Name}}}
	for _, name := range cfg.ConfigNames() {
		if cfg.Configs[name].Profile == p.Name {
			entries = append(entries, Entry{Label: "  " + name + "  (config)", Item: Item{Kind: KindConfig, Profile: p.Name, Config: name}})
		}
	}
	return entries
}

func newEntry(account string) Entry {
	label := "+ new profile"
	if account != "" {
		label += " for " + account
	}
	return Entry{Label: label, Item: Item{Kind: KindNew, Account: account}}
}

// Choose prints the menu to w and reads a choice from br. def is the key of
// the item Enter chooses. note, if any, is printed above the menu.
func Choose(groups []Group, br *bufio.Reader, w io.Writer, note, def string) (Item, error) {
	if note != "" {
		fmt.Fprintln(w, note)
	}
	var items []Item
	defNum := 0
	for _, g := range groups {
		fmt.Fprintln(w, g.Heading)
		for _, e := range g.Entries {
			items = append(items, e.Item)
			if e.Item.Key() == def {
				defNum = len(items)
			}
			fmt.Fprintf(w, "  %3d) %s\n", len(items), e.Label)
		}
	}
	for {
		if defNum > 0 {
			fmt.Fprintf(w, "Choose 1-%d [%d], q to quit: ", len(items), defNum)
		} else {
			fmt.Fprintf(w, "Choose 1-%d, q to quit: ", len(items))
		}
		line, err := br.ReadString('\n')
		ans := strings.TrimSpace(line)
		if err != nil && ans == "" {
			return Item{}, ErrCancelled // EOF or a read error is not a choice, not even the default
		}
		switch {
		case ans == "q":
			return Item{}, ErrCancelled
		case ans == "" && defNum > 0:
			return items[defNum-1], nil
		case ans == "":
			return Item{}, ErrCancelled
		}
		if n, convErr := strconv.Atoi(ans); convErr == nil && n >= 1 && n <= len(items) {
			return items[n-1], nil
		}
		fmt.Fprintf(w, "%q is not a choice.\n", ans)
		if err != nil {
			return Item{}, ErrCancelled
		}
	}
}

// Prompt asks a question and returns the trimmed answer; an empty answer
// cancels.
func Prompt(br *bufio.Reader, w io.Writer, question string) (string, error) {
	fmt.Fprint(w, question)
	line, _ := br.ReadString('\n')
	ans := strings.TrimSpace(line)
	if ans == "" {
		return "", ErrCancelled
	}
	return ans, nil
}

// StatePath is where the last choice is remembered.
func StatePath(env map[string]string, home string) string {
	base := env["XDG_STATE_HOME"]
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claude-profile", "last.json")
}

type lastFile struct {
	Last string `json:"last"`
}

// LoadLast returns the remembered choice's key, or "".
func LoadLast(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var l lastFile
	if json.Unmarshal(data, &l) != nil {
		return ""
	}
	return l.Last
}

// SaveLast remembers a choice's key.
func SaveLast(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(lastFile{Last: key})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
