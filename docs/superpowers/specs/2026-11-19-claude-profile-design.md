# claude-profile: design

Date: 2026-11-19. Status: approved in conversation, awaiting review of this
document.

## Purpose

Tim runs Claude Code under three accounts (`me@example.com`, `claude@example.com`,
`claude@example.org`) across several isolated Claude config directories, with
dozens of interactive sessions alive at once on one machine. Today:

- nothing maps a session to the config directory (and so the account) it
  belongs to, so `claude --resume <id>` only works from an environment that
  already has the right `CLAUDE_CONFIG_DIR`;
- launch presets are hand-written shell scripts (`claude-ac`,
  `claude-ac-coordinator`, `claude-bl`, `claude-bl-coordinator`);
- every session runs in an unlimited cgroup;
- Claude's temp files land on `/tmp`, an 8 GB RAM-backed tmpfs.

`claude-profile` is a small Go launcher that picks the right config directory,
applies a named preset, runs Claude in a resource-limited cgroup and points
its temp files at disk.

## Vocabulary

- **Profile**: one Claude config directory with its own `/login`. Credentials
  are never copied or shared between profiles.
- **Account**: the email a profile is currently logged into. Detected, never
  stored. A profile can move to another account at any time.
- **Config**: a named set of launch arguments (target profile, arguments,
  environment, `PATH` prefix, limit overrides).

## Scope

In scope: the launcher, profile and account discovery, resume lookup, the
interactive menu, cgroup placement, temp directory, `setup`, the query
interfaces other tools need, the repository, Debian packaging and CI.

Out of scope, each a later spec:

1. Changing `go-tmux-saver`, `claude-sessions-backup.py`, `claude-teleport`
   and the scratch-repo launchers to use this tool.
2. Remote-control sessions bound to a dedicated Chrome profile and extension.
3. Showing usage from `claude-usage` in the menu.
4. A hard block on writing to `/tmp` (needs a privileged component; see
   "Temp directory").
5. Seeding a new profile's plugins and settings (what
   `setup_claude_config.py` does today).

## Repository

- Repo `mithro/go-claude-profile`, public, Apache-2.0. Package and binary
  `claude-profile`. Module `github.com/mithro/go-claude-profile`.
- Modelled on `go-tmux-saver`: Linux only, standard library only, stdlib
  `flag`, `cmd/claude-profile/main.go` calling `cli.RunMultiCall`, logic under
  `internal/`, `var Version = "dev"` set by ldflags, a small `Makefile`.
- Packaging and CI copied from `go-tmux-saver` `origin/main`: `nfpm.yaml`,
  `.github/apt-packaging.toml` (`kind = "B"`), `.github/workflows/deb.yml`,
  `packaging/release-assets.sh` and its test. Suites trixie, forky, sid, plus
  the raspbian suites on armhf; architectures amd64, i386, arm64, armhf,
  riscv64. The apt repository is published at
  `https://mith.ro/go-claude-profile/` by `mithro/apt-repo-action`.
- The package installs `/usr/bin/claude-profile`, the README and the
  copyright file. Nothing else: per-user files are written by
  `claude-profile setup install`.
- GitHub settings match the two existing repos: merge commits only, delete
  branch on merge, the `vX.Y` / `build-*` tag ruleset, Pages built by
  workflow, the `github-pages` environment limited to `main`, secret scanning
  with push protection, one secret `APT_GPG_PRIVATE_KEY` holding a new
  RSA 4096 signing key made for this repo.

## Profiles and accounts

### Discovery

- `~/.claude` is the profile named `default`.
- Each directory `~/.claude-<name>` containing `.claude.json` or
  `.credentials.json` is the profile `<name>`.
- The tool config can add directories outside this pattern
  (`extra_profiles`: name to path) and hide discovered ones
  (`ignore_profiles`, for example `old`).
- Profile names match `^[a-z0-9][a-z0-9-]*$`. `default` and `tmp` are
  reserved.

### Config directory handed to Claude

- For `default`, `CLAUDE_CONFIG_DIR` is removed from the environment. Claude
  then reads `~/.claude.json`. Setting the variable to `~/.claude` would make
  Claude read `~/.claude/.claude.json` instead, which is a different file.
- For every other profile, `CLAUDE_CONFIG_DIR` is set to the profile
  directory and Claude reads `<dir>/.claude.json`.

### Account detection

- The account is `oauthAccount.emailAddress` from the profile's
  `.claude.json` (the file chosen by the rule above). The file is read on
  every invocation. A missing file or field means "not logged in".
- A profile may declare an expected account in the tool config. When the
  detected account differs, the menu marks the profile and every launch
  prints a warning on stderr. The launch still proceeds.

### Creating a profile

`claude-profile new <name> [--account <email>]`:

1. Refuses if the name is invalid, reserved or already exists.
2. Creates `~/.claude-<name>` with mode 0700.
3. Runs `claude auth login --claudeai [--email <email>]` with
   `CLAUDE_CONFIG_DIR` set to the new directory, on the user's terminal.
4. Reads the resulting account. If `--account` was given and it differs,
   prints a warning naming both.

## Tool config

`~/.config/claude-profile/config.json` (respects `XDG_CONFIG_HOME`). A
missing file means all defaults. Unknown keys are an error.

```json
{
  "accounts": ["me@example.com", "claude@example.com", "claude@example.org"],
  "claude_path": "",
  "tmp_dir": "~/.claude-tmp",
  "link_dir": "~/bin",
  "ignore_profiles": ["old"],
  "extra_profiles": {},
  "session_limits": {
    "MemoryHigh": "6G", "MemoryMax": "8G",
    "MemorySwapMax": "2G", "TasksMax": "4096"
  },
  "profiles": {
    "west-corp": {
      "account": "claude@example.org",
      "slice_limits": {"MemoryMax": "24G"},
      "session_limits": {}
    }
  },
  "configs": {
    "claude-ac-coordinator": {
      "profile": "west-corp",
      "args": ["--model", "fable", "--name", "ac-coord"],
      "env": {"AC_SESSION": "ac-coord"},
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "opus"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"],
      "session_limits": {}
    },
    "claude-ac": {
      "profile": "west-corp",
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"],
      "name_arg": {"env": "AC_SESSION", "prefix": "ac-"}
    },
    "claude-bl": {
      "profile": "beta-lab",
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"},
      "path_prepend": ["~/src/beta/scratch/bin/ssh-wrappers"],
      "require_env": ["BL_SESSION"]
    }
  }
}
```

- `accounts`: accounts the menu always shows, even with no profile logged
  into them.
- `session_limits`, `slice_limits`: systemd resource-control properties,
  passed through by name. Only names in a fixed allow-list are accepted:
  `MemoryHigh`, `MemoryMax`, `MemorySwapMax`, `TasksMax`, `CPUQuota`,
  `CPUWeight`. Session limits merge in the order top level, profile, config.
- Config fields:
  - `profile` (required).
  - `args`: placed before the user's arguments.
  - `env`: always set. `env_default`: set only when the variable is unset.
  - `path_prepend`: directories put at the front of `PATH`.
  - `require_env`: launch fails with a usage error if any is unset.
  - `name_arg`: reproduces `claude-ac NAME`. If the named variable is unset,
    the first user argument is consumed as NAME, the variable is set to
    prefix + NAME and `--name <prefix><NAME>` is added. If the variable is
    already set, nothing is consumed or added.
  - `session_limits`: overrides.
- A leading `~/` in any path is expanded to the home directory.

## Command line

The binary dispatches on the name it was invoked as.

### Invoked as `claude-profile`: management

- `list [--json]`: accounts, profiles, config directories, detected and
  expected accounts, configs.
- `new <name> [--account <email>]`: see above.
- `which [--json] <session-uuid | session-name | pid>`: the profile, config
  directory and account that own a session.
- `run [--profile <p> | --config <c>] [--] <claude args>`: launch with an
  explicit selection.
- `setup install | status | uninstall`: per-user files, see "Setup".
- `doctor`: checks listed under "Doctor".
- `version`, `--version`, `help`.

Exit codes follow `go-tmux-saver`: 2 for usage or configuration errors, 1 for
failures, otherwise Claude's own exit status (the process is replaced by
Claude).

### Invoked under any other name: launcher

Every argument is passed to Claude unchanged (apart from `name_arg`). If the
name matches a config, that config is used. Otherwise it is the generic
launcher. This allows a later `claude` symlink earlier on `PATH`; `setup`
never creates one.

### Selecting the profile

First match wins:

1. `run --profile` or `run --config`.
2. A config matching the invoked name.
3. Resume lookup, when the arguments name a session.
4. `CLAUDE_PROFILE` inherited from the environment (a launch from inside a
   wrapped session).
5. The interactive menu.

If 1 or 2 selected a profile and the resume lookup finds the session only in
a different profile, the launch stops with a usage error naming both
profiles. If nothing selects a profile and there is no terminal, the launch
stops with a usage error listing the profiles.

### Resume lookup

- Triggered by `--resume <v>`, `--resume=<v>` or `-r <v>` where `<v>` does not
  start with `-`. `--resume` with no value and `--continue` do not name a
  session, so they do not trigger it.
- UUID: glob `projects/*/<uuid>.jsonl` in every profile. Each hit is resolved
  with `EvalSymlinks` and attributed to the profile whose real directory
  contains it, so the back-symlinks under `~/.claude/projects` point at their
  true owners. Hits that resolve to the same file count once.
- Name: match against `name` in each profile's `sessions/*.json` registry,
  then against names recorded in transcripts. How Claude records names for
  sessions that are not running is to be established by reading real
  transcripts during implementation; the lookup is isolated in one function
  with fixtures for whatever is found.
- One owning profile: use it. None, or more than one: fall to the menu and
  list the candidates above it. With no terminal, stop with a usage error.

### Menu

- Plain numbered list on the terminal, standard library only, written to
  stderr and read from `/dev/tty`.
- Grouped by account: detected accounts, then pinned accounts with no
  profile, then "not logged in". Within a group: each profile, each config
  under its profile, and "new profile under this account".
- The entry used last time is the default (stored in
  `$XDG_STATE_HOME/claude-profile/last.json`).
- Choosing "new profile" asks for a name and runs the `new` flow, then
  launches in it.

## Launch

1. Resolve the real Claude binary: `claude_path` if set, otherwise the first
   `claude` on `PATH` that does not resolve to this executable.
2. Build the environment: `CLAUDE_CONFIG_DIR` per the rule above,
   `CLAUDE_PROFILE=<name>`, `CLAUDE_CODE_TMPDIR` and `TMPDIR`, then the
   config's `env`, `env_default` and `path_prepend`.
3. Print the expected-account warning if it applies.
4. Make sure the slice unit files are current (see below).
5. Replace the process with:

   ```
   systemd-run --user --scope --collect --quiet --same-dir
     --slice=<profile slice> --description="Claude <profile> [<config>]"
     -p <session limits...> -- <claude> <config args> <user args>
   ```

   `systemd-run --scope` runs the command as the calling process, so the
   terminal, PID and exit status behave as with a direct launch.

- If the process is already inside a scope under the same profile slice, the
  tool execs Claude directly rather than nesting a second scope.
- `CLAUDE_PROFILE_NO_CGROUP=1` skips the cgroup and execs Claude directly,
  with a warning. Without it, a failure to start the scope is an error.

### cgroup layout

```
user@<uid>.service/claude.slice
  claude-<profile>.slice        one per profile, slice_limits
    run-….scope                 one per session, session_limits
```

- Profile names are escaped as `systemd-escape` does, so a dash in a name
  does not create an extra slice level.
- Slice units are files in `~/.config/systemd/user/`, each starting with a
  "managed by claude-profile" marker line. The tool rewrites a file only if
  it carries the marker, and runs `systemctl --user daemon-reload` after a
  change. A file without the marker is left alone and reported.
- Limits given as percentages are resolved by systemd when the unit is
  loaded; they do not follow later changes in RAM from the balloon driver.

### Temp directory

- `CLAUDE_CODE_TMPDIR` and `TMPDIR` are both set to `tmp_dir`, one directory
  shared by all profiles, created with mode 0700. Claude adds
  `claude-<uid>/<project>/<session>` beneath it.
- The tool refuses a `tmp_dir` that is on tmpfs or where
  `<tmp_dir>/claude-<uid>` is longer than 44 bytes, the limit documented by
  the `hook-block-tmp-creation` plugin. This is why there is no per-profile
  level: `~/.claude-tmp/west-corp/claude-1000` is 45 bytes.
- `setup install` writes a user `tmpfiles.d` rule that removes entries
  untouched for 14 days.
- Programs that hard-code `/tmp` are not stopped. Measured on this machine:
  tmpfs pages are charged to the writing cgroup, so the session's memory
  limit caps such writes, killing the writer at the limit and leaving the
  partial file behind. Every unprivileged way of blocking `/tmp` outright
  (Landlock or seccomp via `no_new_privs`, user namespaces, `PrivateTmp` or
  `BindPaths` in a user service) breaks `sudo` inside the session, so a hard
  block is deferred.

## Setup

`claude-profile setup install` is idempotent and writes only per-user files:

- `claude.slice` and one `claude-<profile>.slice` per discovered profile;
- the `tmpfiles.d` rule and the temp directory;
- one symlink per config in `link_dir`, pointing at the installed binary. An
  existing path is replaced only if it is already a symlink to this binary;
  anything else is reported and left alone.

`setup status` reports what differs. `setup uninstall` removes only files
that carry the marker and symlinks that point at this binary.

## Doctor

Reports pass or fail for: the real Claude binary is found; `systemd-run
--user` works; `cpu`, `memory` and `pids` are delegated to the user manager;
the temp directory exists, is not tmpfs and is short enough; slice units are
current; each profile's detected account against its expected account; every
config names an existing profile.

## Interfaces for other tools

- `CLAUDE_PROFILE` in the environment of every wrapped Claude process.
- `claude-profile which <session-uuid | session-name | pid>`. A PID is
  resolved from `CLAUDE_CONFIG_DIR` in `/proc/<pid>/environ`.
- `claude-profile list --json`.
- `claude-profile run --profile <p> -- --resume <id>` as the stable way to
  resume in a known profile.

The JSON output of `which` and `list` is documented in the README and
covered by tests, since later work in other repositories depends on it.

## Code layout

- `cmd/claude-profile/main.go`: one line into `cli.RunMultiCall`.
- `internal/cli`: dispatch, flag parsing, the only package that reads the
  process environment and arguments.
- `internal/config`: load and validate `config.json`.
- `internal/profile`: discovery, account detection, creation.
- `internal/resume`: argument scanning and session lookup.
- `internal/menu`: rendering and choice parsing, with injected reader and
  writer.
- `internal/launch`: environment, `systemd-run` argument list, exec.
- `internal/slice`: unit naming, escaping, rendering, sync.
- `internal/setup`: install, status, uninstall, doctor checks.

Each package takes its inputs (home directory, environment, readers,
writers, an exec function) as parameters so it can be tested without
touching the real home directory.

## Error handling

- Errors print as `claude-profile: <message>` on stderr and are wrapped with
  `%w`.
- A broken `config.json` stops every command except `help` and `version`.
- An unreadable or malformed profile `.claude.json` makes that profile "not
  logged in" with a note in `list` and `doctor`; it never stops other
  profiles from working.
- The tool never writes inside a profile directory except to create it in
  `new`.

## Testing

- Table tests over fixture trees under `testdata`: profile directories with
  and without logins, the `default` special case, back-symlinked project
  directories, session registries, transcripts.
- `internal/launch` tests assert the exact environment and `systemd-run`
  argument list through an injected exec function.
- Menu tests drive a scripted reader.
- `setup` tests run against a temporary home directory.
- One opt-in test (build tag `systemdlive`) starts a real scope with a fake
  `claude` and reads back its cgroup limits.
- CI: `go vet`, `gofmt` check, `go test -race`, the release-assets test, and
  the per-suite install test asserting `claude-profile version`.
