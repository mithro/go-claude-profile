package cli

import (
	"fmt"

	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/setup"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/tmpdir"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"setup", "manage per-user files: setup install|status|uninstall", cmdSetup})
}

func cmdSetup(args []string, d Deps) int {
	if len(args) != 1 {
		return fail(d, usage.Errorf("usage: %s setup install|status|uninstall", toolName))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	switch args[0] {
	case "install":
		return setupApply(d, w, false)
	case "status":
		return setupApply(d, w, true)
	case "uninstall":
		return setupUninstall(d, w)
	}
	return fail(d, usage.Errorf("usage: %s setup install|status|uninstall", toolName))
}

// setupApply installs (or, with dryRun, reports) the slice units, the
// tmpfiles.d rule and the config symlinks. With dryRun it fails if anything
// would change; either way it fails if a path is not ours.
func setupApply(d Deps, w *world, dryRun bool) int {
	if err := d.CheckTmp(w.cfg.TmpDir, d.UID); err != nil {
		return fail(d, usage.Errorf("%v", err))
	}
	if !dryRun {
		if err := tmpdir.Ensure(w.cfg.TmpDir); err != nil {
			return fail(d, err)
		}
	}
	units, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), dryRun)
	if err != nil {
		return fail(d, err)
	}
	rule, err := managed.Sync(setup.TmpfilesDir(d.Env, w.home),
		map[string]string{setup.TmpfilesName: setup.TmpfilesContent(w.cfg.TmpDir)}, dryRun)
	if err != nil {
		return fail(d, err)
	}
	links, err := setup.SyncLinks(w.cfg.LinkDir, d.Self, w.cfg.ConfigNames(), dryRun)
	if err != nil {
		return fail(d, err)
	}
	written, linked := "wrote", "linked"
	if dryRun {
		written, linked = "would write", "would link"
	}
	report := func(status string, paths []string) {
		for _, p := range paths {
			fmt.Fprintf(d.Stdout, "%-12s %s\n", status, p)
		}
	}
	for _, r := range []managed.Result{units, rule} {
		report(written, r.Written)
		report("ok", r.Unchanged)
		report("not managed", r.Foreign)
	}
	report(linked, links.Created)
	report("ok", links.Unchanged)
	report("not managed", links.Foreign)

	if !dryRun && units.Changed() {
		if err := reloadUnits(d, units.Written); err != nil {
			return fail(d, err)
		}
	}
	bad := len(units.Foreign)+len(rule.Foreign)+len(links.Foreign) > 0
	if dryRun && (units.Changed() || rule.Changed() || len(links.Created) > 0) {
		bad = true
	}
	if bad {
		return exitFail
	}
	return exitOK
}

func setupUninstall(d Deps, w *world) int {
	units, err := managed.Remove(slice.UnitDir(d.Env, w.home), "claude*.slice")
	if err != nil {
		return fail(d, err)
	}
	rules, err := managed.Remove(setup.TmpfilesDir(d.Env, w.home), setup.TmpfilesName)
	if err != nil {
		return fail(d, err)
	}
	links, err := setup.RemoveLinks(w.cfg.LinkDir, d.Self, w.cfg.ConfigNames())
	if err != nil {
		return fail(d, err)
	}
	for _, group := range [][]string{units, rules, links} {
		for _, p := range group {
			fmt.Fprintf(d.Stdout, "%-12s %s\n", "removed", p)
		}
	}
	fmt.Fprintf(d.Stdout, "The temp directory %s is left in place.\n", w.cfg.TmpDir)
	if len(units) > 0 {
		if err := d.Run([]string{"systemctl", "--user", "daemon-reload"}, launch.EnvList(d.Env), nil, d.Stderr, d.Stderr); err != nil {
			return fail(d, err)
		}
	}
	return exitOK
}
