package cli

import (
	"bufio"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/config"
	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/menu"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/resume"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/tmpdir"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"run", "launch Claude: run [--profile P | --config C] [--] [claude args]", cmdRun})
	register(command{"new", "create a profile and log it in: new [--account EMAIL] NAME", cmdNew})
}

type launchOpts struct {
	invoked string // the name the program was invoked as
	profile string // run --profile
	config  string // run --config
	args    []string
}

func runLauncher(name string, args []string, d Deps) int {
	return launchClaude(d, launchOpts{invoked: name, args: args})
}

func cmdRun(args []string, d Deps) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	prof := fs.String("profile", "", "profile to launch in")
	cfgName := fs.String("config", "", "config to launch with")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	if *prof != "" && *cfgName != "" {
		return fail(d, usage.Errorf("give --profile or --config, not both"))
	}
	return launchClaude(d, launchOpts{invoked: toolName, profile: *prof, config: *cfgName, args: fs.Args()})
}

func launchClaude(d Deps, o launchOpts) int {
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	profName, cfgName, err := selectProfile(d, w, o)
	if err != nil {
		return fail(d, err)
	}
	p, ok := profile.Find(w.profiles, profName)
	if !ok {
		return fail(d, usage.Errorf("no profile named %q (see %s list)", profName, toolName))
	}
	var c *config.Config
	if cfgName != "" {
		cc := w.cfg.Configs[cfgName]
		c = &cc
	}
	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	if err != nil {
		return fail(d, err)
	}
	if err := d.CheckTmp(w.cfg.TmpDir, d.UID); err != nil {
		return fail(d, usage.Errorf("%v", err))
	}
	if err := tmpdir.Ensure(w.cfg.TmpDir); err != nil {
		return fail(d, err)
	}
	cgroup, _ := os.ReadFile(filepath.Join(d.ProcRoot, "self", "cgroup"))
	systemdRun := ""
	if launch.WantScope(d.Env, string(cgroup), p.Name) {
		if systemdRun, err = d.LookPath("systemd-run"); err != nil {
			return fail(d, fmt.Errorf("systemd-run: %w (set %s=1 to run without limits)", err, launch.NoCgroupEnv))
		}
		if err := syncSlices(d, w); err != nil {
			return fail(d, err)
		}
	}
	plan, err := launch.Build(launch.Request{
		Env: d.Env, Profile: p, ConfigName: cfgName, Config: c, Args: o.args,
		Claude: claude, SystemdRun: systemdRun, TmpDir: w.cfg.TmpDir,
		Limits: w.cfg.SessionLimitsFor(p.Name, cfgName),
	})
	if err != nil {
		return fail(d, err)
	}
	for _, msg := range plan.Warnings {
		warn(d, "%s", msg)
	}
	err = d.Exec(plan.Argv[0], plan.Argv, plan.Env)
	return fail(d, fmt.Errorf("exec %s: %w", plan.Argv[0], err))
}

// selectProfile applies the spec's order: run --profile/--config, a config
// named like the invoked name, the profile owning a --resume session, an
// inherited CLAUDE_PROFILE, then the menu.
func selectProfile(d Deps, w *world, o launchOpts) (profName, cfgName string, err error) {
	profName, cfgName = o.profile, o.config
	if cfgName == "" && o.invoked != toolName {
		if _, ok := w.cfg.Configs[o.invoked]; ok {
			cfgName = o.invoked
		}
	}
	if cfgName != "" {
		c, ok := w.cfg.Configs[cfgName]
		if !ok {
			return "", "", usage.Errorf("no config named %q", cfgName)
		}
		profName = c.Profile
	}
	if profName != "" {
		if _, ok := profile.Find(w.profiles, profName); !ok {
			return "", "", usage.Errorf("no profile named %q (see %s list)", profName, toolName)
		}
	}
	v, resuming := resume.Value(o.args)
	if !resuming {
		if profName != "" {
			return profName, cfgName, nil
		}
		if inherited := d.Env["CLAUDE_PROFILE"]; inherited != "" {
			if _, ok := profile.Find(w.profiles, inherited); ok {
				return inherited, "", nil
			}
		}
		return chooseInteractively(d, w, "")
	}
	hits, err := resume.Lookup(w.profiles, v)
	if err != nil {
		return "", "", err
	}
	owners := hitOwners(hits)
	switch {
	case profName != "":
		if len(owners) > 0 && !slices.Contains(owners, profName) {
			return "", "", usage.Errorf("session %s belongs to profile %s, not %s", v, strings.Join(owners, ", "), profName)
		}
		return profName, cfgName, nil
	case len(owners) == 1:
		return owners[0], "", nil
	case len(owners) == 0:
		return chooseInteractively(d, w, fmt.Sprintf("No profile has a session %q.", v))
	default:
		return chooseInteractively(d, w, fmt.Sprintf("Session %q exists in several profiles: %s.", v, strings.Join(owners, ", ")))
	}
}

func hitOwners(hits []resume.Hit) []string {
	var owners []string
	for _, h := range hits {
		if !slices.Contains(owners, h.Profile) {
			owners = append(owners, h.Profile)
		}
	}
	slices.Sort(owners)
	return owners
}

func chooseInteractively(d Deps, w *world, note string) (string, string, error) {
	tty, err := d.OpenTTY()
	if err != nil {
		msg := fmt.Sprintf("no profile selected and no terminal for the menu; use %s run --profile NAME (profiles: %s)",
			toolName, strings.Join(profileNames(w.profiles), ", "))
		if note != "" {
			msg = note + " " + msg
		}
		return "", "", usage.Errorf("%s", msg)
	}
	defer tty.Close()
	br := bufio.NewReader(tty)
	statePath := menu.StatePath(d.Env, w.home)
	item, err := menu.Choose(menu.Build(w.profiles, w.cfg), br, d.Stderr, note, menu.LoadLast(statePath))
	if err != nil {
		return "", "", err
	}
	if item.Kind == menu.KindNew {
		name, err := menu.Prompt(br, d.Stderr, "Name for the new profile: ")
		if err != nil {
			return "", "", err
		}
		if err := createProfile(d, w, name, item.Account); err != nil {
			return "", "", err
		}
		item = menu.Item{Kind: menu.KindProfile, Profile: name}
	}
	if err := menu.SaveLast(statePath, item.Key()); err != nil {
		warn(d, "could not remember the choice: %v", err)
	}
	return item.Profile, item.Config, nil
}

// syncSlices brings the slice units up to date and reloads systemd if any
// changed.
func syncSlices(d Deps, w *world) error {
	res, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), false)
	if err != nil {
		return err
	}
	for _, f := range res.Foreign {
		warn(d, "%s is not managed by %s; leaving it alone", f, toolName)
	}
	if res.Changed() {
		return d.Run([]string{"systemctl", "--user", "daemon-reload"}, launch.EnvList(d.Env), nil, d.Stderr, d.Stderr)
	}
	return nil
}

func cmdNew(args []string, d Deps) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	account := fs.String("account", "", "the account (email) to log in as")
	if code, ok := parseFlags(fs, args); !ok {
		return code
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fail(d, usage.Errorf("usage: %s new [--account EMAIL] NAME", toolName))
	}
	name := rest[0]
	if code, ok := parseFlags(fs, rest[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail(d, usage.Errorf("usage: %s new [--account EMAIL] NAME", toolName))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	if err := createProfile(d, w, name, *account); err != nil {
		return fail(d, err)
	}
	return exitOK
}

// createProfile makes ~/.claude-<name> and runs Claude's own login in it. No
// credentials are copied from anywhere.
func createProfile(d Deps, w *world, name, account string) error {
	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	if err != nil {
		return err
	}
	dir, err := profile.Create(w.home, name)
	if err != nil {
		return err
	}
	argv := []string{claude, "auth", "login", "--claudeai"}
	if account != "" {
		argv = append(argv, "--email", account)
	}
	env := maps.Clone(d.Env)
	env["CLAUDE_CONFIG_DIR"] = dir
	fmt.Fprintf(d.Stderr, "Logging in profile %s (%s)...\n", name, dir)
	if err := d.Run(argv, launch.EnvList(env), d.Stdin, d.Stdout, d.Stderr); err != nil {
		if os.Remove(dir) == nil {
			return fmt.Errorf("login for profile %s failed: %w (removed the empty %s)", name, err, dir)
		}
		return fmt.Errorf("login for profile %s failed: %w (%s is left as it is; retry with CLAUDE_CONFIG_DIR=%s claude auth login)", name, err, dir, dir)
	}
	got, note := profile.ReadAccount(filepath.Join(dir, ".claude.json"))
	switch {
	case got == "":
		return fmt.Errorf("profile %s is still not logged in after the login %s", name, note)
	case account != "" && !strings.EqualFold(got, account):
		warn(d, "profile %s logged in as %s, not %s", name, got, account)
	default:
		fmt.Fprintf(d.Stderr, "Profile %s is logged in as %s.\n", name, got)
	}
	w.profiles, err = profile.Discover(w.home, w.cfg)
	return err
}
