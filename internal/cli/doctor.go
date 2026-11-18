package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mithro/go-claude-profile/internal/launch"
	"github.com/mithro/go-claude-profile/internal/managed"
	"github.com/mithro/go-claude-profile/internal/profile"
	"github.com/mithro/go-claude-profile/internal/slice"
	"github.com/mithro/go-claude-profile/internal/usage"
)

func init() {
	register(command{"doctor", "check that launching will work", cmdDoctor})
}

func cmdDoctor(args []string, d Deps) int {
	if len(args) != 0 {
		return fail(d, usage.Errorf("doctor takes no arguments"))
	}
	w, err := load(d)
	if err != nil {
		return fail(d, err)
	}
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(d.Stdout, "FAIL  %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(d.Stdout, "ok    %s\n", name)
	}

	claude, err := launch.ResolveClaude(w.cfg.ClaudePath, d.Env["PATH"], d.Self)
	check("claude binary "+claude, err)
	if path, err := d.LookPath("systemd-run"); err != nil {
		check("systemd-run --user", err)
	} else {
		check("systemd-run --user", d.Run([]string{path, "--user", "--scope", "--collect", "--quiet", "true"},
			launch.EnvList(d.Env), nil, d.Stdout, d.Stdout))
	}
	check("cgroup controllers delegated", checkControllers(d))
	check("temp directory "+w.cfg.TmpDir, d.CheckTmp(w.cfg.TmpDir, d.UID))
	check("slice units", checkSlices(d, w))
	if err := d.Run([]string{"systemctl", "--user", "is-active", "--quiet", tmpfilesTimer},
		launch.EnvList(d.Env), nil, d.Stdout, d.Stdout); err != nil {
		check("tmpfiles cleanup timer", fmt.Errorf("%w (run: %s)", err, strings.Join(tmpfilesTimerEnable, " ")))
	} else {
		check("tmpfiles cleanup timer", nil)
	}
	for _, p := range w.profiles {
		switch {
		case p.Mismatch():
			check("profile "+p.Name, fmt.Errorf("logged in as %s, expected %s", p.Account, p.Expected))
		case p.Account == "":
			fmt.Fprintf(d.Stdout, "note  profile %s is not logged in %s\n", p.Name, p.Note)
		default:
			check("profile "+p.Name+" ("+p.Account+")", nil)
		}
	}
	for _, name := range w.cfg.ConfigNames() {
		var err error
		if c := w.cfg.Configs[name]; !hasProfile(w.profiles, c.Profile) {
			err = fmt.Errorf("names profile %q, which does not exist", c.Profile)
		}
		check("config "+name, err)
	}
	if failed {
		return exitFail
	}
	return exitOK
}

func hasProfile(ps []profile.Profile, name string) bool {
	_, ok := profile.Find(ps, name)
	return ok
}

func checkControllers(d Deps) error {
	path := filepath.Join(d.CgroupRoot, "user.slice", fmt.Sprintf("user-%d.slice", d.UID),
		fmt.Sprintf("user@%d.service", d.UID), "cgroup.controllers")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	have := strings.Fields(string(data))
	var missing []string
	for _, c := range []string{"cpu", "memory", "pids"} {
		if !slices.Contains(have, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("not delegated to the user manager: %s", strings.Join(missing, ", "))
	}
	return nil
}

func checkSlices(d Deps, w *world) error {
	res, err := managed.Sync(slice.UnitDir(d.Env, w.home), slice.Desired(w.profiles, w.cfg), true)
	if err != nil {
		return err
	}
	if len(res.Foreign) > 0 {
		return fmt.Errorf("not managed by %s: %s", toolName, strings.Join(res.Foreign, ", "))
	}
	if res.Changed() {
		return errors.New("out of date; run claude-profile setup install")
	}
	return nil
}
