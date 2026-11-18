# claude-profile

Launch [Claude Code](https://claude.com/claude-code) in the right **profile**
(a Claude Code config directory with its own login) for the right
**account**, inside a resource-limited systemd user scope, with its temp
files on disk instead of a RAM-backed `/tmp`.

- `claude-profile run -- --resume <id>` finds which profile owns the session
  and launches it there.
- With nothing to go on, it shows a menu of profiles grouped by the account
  each is logged into.
- **Configs** are named sets of launch arguments (model, environment, `PATH`
  prefix, limits). A symlink named after a config is a ready-made launcher:
  `claude-ac-coordinator` → `claude-profile`.

## Install

```
sudo install -d -m0755 /etc/apt/keyrings
curl -fsSL https://mith.ro/go-claude-profile/go-claude-profile.gpg | sudo tee /etc/apt/keyrings/go-claude-profile.gpg > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/go-claude-profile.gpg] https://mith.ro/go-claude-profile/trixie/ ./" \
  | sudo tee /etc/apt/sources.list.d/go-claude-profile.list
sudo apt update
sudo apt install claude-profile
```

The suites are `trixie`, `forky` and `sid`, plus `raspbian-trixie` and
`raspbian-forky`; use the one matching your system in the `deb` line.

`claude-profile version` prints the version without the suite suffix.

Every push to `main` is a build: it publishes the packages to the apt
repository and creates a GitHub Release tagged `build-<version>`. The
`vX.Y` tags are made by hand.

Then, once per user:

```
claude-profile setup install
claude-profile doctor
```

### Build from source

```
make build      # ./claude-profile
```

## Usage

| Command | What it does |
|---|---|
| `claude-profile run [--profile P \| --config C] [--] [claude args]` | Launch Claude. |
| `claude-profile list [--json]` | Accounts, profiles, config directories, configs. |
| `claude-profile which [--json] <session-uuid \| session-name \| pid>` | The profile, config directory and account that own a session or a running Claude. |
| `claude-profile new [--account EMAIL] NAME` | Create `~/.claude-NAME` and run Claude's own login in it. |
| `claude-profile setup install \| status \| uninstall` | Per-user files: slice units, the temp-dir cleanup rule, config symlinks. |
| `claude-profile doctor` | Check that launching will work. |

Invoked under any other name, the binary is a launcher and passes every
argument to Claude. If the name is a config's name, that config is used.

### How the profile is chosen

1. `run --profile` or `run --config`.
2. A config named like the command (the symlink).
3. For `--resume <id>`, `--resume=<id>` or `-r <id>`: the profile that owns
   that session, by UUID or by session name.
4. `CLAUDE_PROFILE` inherited from the environment (a launch from inside a
   wrapped session), used only when no profile or config was chosen by 1 or
   2 and the arguments do not name a session to resume (3).
5. The menu. Without a terminal, the launch stops with an error listing the
   profiles.

If 1 or 2 picked a profile but the resumed session belongs to another one,
the launch stops with an error naming both.

## Profiles and accounts

- `~/.claude` is the profile `default`; it is launched with
  `CLAUDE_CONFIG_DIR` **unset**, so Claude reads `~/.claude.json`.
- Every `~/.claude-<name>` holding `.claude.json` or `.credentials.json` is
  the profile `<name>`, launched with `CLAUDE_CONFIG_DIR=~/.claude-<name>`.
- A profile's account is read from `oauthAccount.emailAddress` in its
  `.claude.json` every time. Nothing stores it, so a profile that gets
  logged into another account simply moves to that account's group.
- Credentials are never copied between profiles: each one does its own
  `/login`.

## Configuration

`~/.config/claude-profile/config.json` (or under `$XDG_CONFIG_HOME`). It is
optional; unknown keys are an error.

```json
{
  "accounts": ["me@example.com", "claude@example.com", "claude@example.org"],
  "ignore_profiles": ["old"],
  "profiles": {
    "west-corp": {"account": "claude@example.org", "slice_limits": {"MemoryMax": "24G"}}
  },
  "configs": {
    "claude-ac-coordinator": {
      "profile": "west-corp",
      "args": ["--model", "fable", "--name", "ac-coord"],
      "env": {"AC_SESSION": "ac-coord"},
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "opus"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"]
    },
    "claude-ac": {
      "profile": "west-corp",
      "env_default": {"CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"},
      "path_prepend": ["~/src/west/scratch/bin/ssh-wrappers"],
      "name_arg": {"env": "AC_SESSION", "prefix": "ac-"}
    }
  }
}
```

| Key | Meaning | Default |
|---|---|---|
| `accounts` | Accounts the menu always shows, even with no profile. | none |
| `claude_path` | The real `claude` binary. | first `claude` on `PATH` that isn't this program |
| `tmp_dir` | Claude's temp directory (`CLAUDE_CODE_TMPDIR`, `TMPDIR`). Must be on disk, and `<tmp_dir>/claude-<uid>` at most 44 bytes. | `~/.claude-tmp` |
| `link_dir` | Where `setup install` puts one symlink per config. | `~/bin` |
| `ignore_profiles` | Discovered profiles to hide. | none |
| `extra_profiles` | Profiles outside `~/.claude-*`: name → directory. | none |
| `session_limits` | Limits for every session's scope. | `MemoryHigh=6G`, `MemoryMax=8G`, `MemorySwapMax=2G`, `TasksMax=4096` |
| `profiles.<name>.account` | The account this profile should be logged into; a mismatch is a warning. | none |
| `profiles.<name>.slice_limits` | Limits for all of the profile's sessions together. | none |
| `profiles.<name>.session_limits` | Per-session overrides for this profile. | none |
| `configs.<name>.profile` | The profile the config launches in. | required |
| `configs.<name>.args` | Arguments placed before the user's. | none |
| `configs.<name>.env` / `env_default` | Variables always set / set only when unset. | none |
| `configs.<name>.path_prepend` | Directories put at the front of `PATH`. | none |
| `configs.<name>.require_env` | Variables that must be set, or the launch fails. | none |
| `configs.<name>.name_arg` | `{"env", "prefix"}`: when `env` is unset, the first argument is NAME, `env=prefix+NAME`, and `--name prefix+NAME` is added. | none |
| `configs.<name>.session_limits` | Per-session overrides for this config. | none |

Limits accept `CPUQuota`, `CPUWeight`, `MemoryHigh`, `MemoryMax`,
`MemorySwapMax` and `TasksMax`, with systemd's syntax.

## How it works

- **cgroups.** A launch replaces itself with
  `systemd-run --user --scope --slice=claude-<profile>.slice -p … -- claude …`,
  so Claude keeps the terminal, PID and exit status. The hierarchy is
  `claude.slice` → `claude-<profile>.slice` → one scope per session.
  `setup install` (and every launch) keeps the slice unit files in
  `~/.config/systemd/user/` current; files without the "Managed by
  claude-profile" first line are never touched. A launch from inside a
  session of the same profile does not nest another scope.
  `CLAUDE_PROFILE_NO_CGROUP=1` skips the scope, with a warning.
- **Temp files.** `CLAUDE_CODE_TMPDIR` and `TMPDIR` point at `tmp_dir`.
  Programs that hard-code `/tmp` are not stopped, but tmpfs pages count
  against the session's memory limit, so they cannot grow without bound. A
  user `tmpfiles.d` rule removes entries untouched for 14 days.
  `setup install` enables the user `systemd-tmpfiles-clean.timer` that
  applies that rule, and `doctor` checks that the timer is active.
- **Session names.** Claude Code records a session's name in its transcript
  (`custom-title` and `agent-name` records, repeated every turn). The lookup
  reads the registry of running sessions, then the last 256 KiB of each
  transcript; a session whose newest name record is further back is not
  found by name, and the menu is shown instead.

## For other tools

- Every wrapped Claude has `CLAUDE_PROFILE=<name>` in its environment.
- `claude-profile which --json <uuid|name|pid>` prints a list of
  objects. `profile`, `config_dir` and `account` are always present (an empty
  string when unknown); `session_id`, `transcript` and `pid` are omitted when
  empty. `pid` appears only for a PID lookup, `transcript` only for a
  UUID or name lookup.
- `claude-profile list --json` prints
  `{"accounts": [...], "profiles": [...], "configs": [...]}`, all three
  always present. Each profile has `name`, `dir`, `default`, `account` and
  `logged_in` always, and `expected_account` and `note` only when non-empty.
  Each config is `{"name", "profile"}`.
- `claude-profile run --profile <p> -- --resume <id>` resumes in a known
  profile.

## Development

```
make test        # unit tests; temp files go to ./tmp, never /tmp
make test-live   # also starts real systemd user scopes
make vet fmt-check
```

Licence: Apache-2.0.
