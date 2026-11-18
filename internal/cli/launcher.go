package cli

import "github.com/mithro/go-claude-profile/internal/usage"

// runLauncher is replaced by the real launcher in Task 10.
func runLauncher(name string, args []string, d Deps) int {
	return fail(d, usage.Errorf("%s: the launcher is not built yet", name))
}
