package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/menu"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"list", "list accounts, profiles and configs [--json]", cmdList})
}

type listOutput struct {
	Accounts []string     `json:"accounts"`
	Profiles []profileOut `json:"profiles"`
	Configs  []configOut  `json:"configs"`
}

type profileOut struct {
	Name            string `json:"name"`
	Dir             string `json:"dir"`
	Default         bool   `json:"default"`
	Account         string `json:"account"`
	ExpectedAccount string `json:"expected_account,omitempty"`
	LoggedIn        bool   `json:"logged_in"`
	Note            string `json:"note,omitempty"`
}

type configOut struct {
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

func cmdList(args []string, d Deps) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail(d, usage.Errorf("list takes no arguments"))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	if *asJSON {
		return writeJSON(d, buildList(w))
	}
	for _, g := range menu.Build(w.profiles, w.cfg) {
		fmt.Fprintln(d.Stdout, g.Heading)
		for _, e := range g.Entries {
			if e.Item.Kind != menu.KindNew {
				fmt.Fprintf(d.Stdout, "  %s\n", e.Label)
			}
		}
	}
	return exitOK
}

func buildList(w *world) listOutput {
	out := listOutput{Accounts: []string{}, Profiles: []profileOut{}, Configs: []configOut{}}
	seen := map[string]bool{}
	add := func(a string) {
		if k := strings.ToLower(a); a != "" && !seen[k] {
			seen[k] = true
			out.Accounts = append(out.Accounts, a)
		}
	}
	for _, p := range w.profiles {
		add(p.Account)
	}
	for _, a := range w.cfg.Accounts {
		add(a)
	}
	slices.SortFunc(out.Accounts, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	for _, p := range w.profiles {
		out.Profiles = append(out.Profiles, profileOut{Name: p.Name, Dir: p.Dir, Default: p.Default, Account: p.Account,
			ExpectedAccount: p.Expected, LoggedIn: p.Account != "", Note: p.Note})
	}
	for _, n := range w.cfg.ConfigNames() {
		out.Configs = append(out.Configs, configOut{Name: n, Profile: w.cfg.Configs[n].Profile})
	}
	return out
}
