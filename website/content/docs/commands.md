---
title: "Commands"
description: "Reference for every romp command and flag."
weight: 3
---

romp is a single binary. Every command runs against the repo in your current
directory (resolved from its `origin` remote) unless noted.

| Command | What it does |
| --- | --- |
| `romp init` | Write `romp.toml`, create the labels, update `.gitignore`. |
| `romp watch` | Register this origin and open the daemon dashboard. |
| `romp unwatch` | Stop new claims; active jobs drain. |
| `romp rebind` | Bind a watched origin to this checkout. |
| `romp daemon` | Install and manage the launchd user agent. |
| `romp run -i N` | Run one issue now, ignoring the label. |
| `romp status` | In-flight jobs. `--all` for every repo. |
| `romp history` | Recently finished jobs. `--all` for every repo. `--review` for reviewer calibration. |
| `romp logs <codename\|issue> [-f]` | Show or follow a job's log. |
| `romp cancel <issue>` | Kill a running job and abandon it. |
| `romp gc` | Remove stale worktrees and old history. Dry-run unless `--apply`. |
| `romp doctor` | Check git, gh auth, harness, config. |
| `romp skills install\|uninstall\|status` | Manage the bundled `rompify` agent skill. |

## `romp skills`

Romp embeds the `rompify` skill in every binary. The skill inspects a repository
and converts a short request or existing GitHub issue into a deterministic
execution contract. It does not implement or run the issue.

```bash
romp skills install                   # install for detected agents
romp skills install --agent codex     # Codex CLI only
romp skills install --agent claude    # Claude Code only
romp skills install --agent openclaw  # OpenClaw only
romp skills install --agent all       # every supported agent
romp skills install --dry-run         # preview exact files
romp skills status
romp skills uninstall --agent codex
```

Install writes `rompify/` below the selected agent's skill directory and records
the binary version in `.romp-version`. Running install again replaces that
directory with the skill bundled in the current binary. Without `--agent`,
install targets detected agent configuration directories. It fails without
writing when no supported agent is detected.

| Agent | Installation path |
| --- | --- |
| Claude Code | `~/.claude/skills/rompify/` |
| Codex CLI | `~/.agents/skills/rompify/` |
| OpenClaw | `~/.openclaw/skills/rompify/` |

## `romp run`

Run one issue now through the daemon, without requiring a trigger label.

```bash
romp run -i 17
```

| Flag | Description |
| --- | --- |
| `-i, --issue` | Issue number to run (required). |
| `--verify` | Command that must pass in the worktree before a PR opens (overrides config). |
| `--no-review` | Skip the review gate for this one run. |
| `--harness` | Harness to use (`claude`, `codex`, or `opencode`). |
| `--model` | Model for the harness. |
| `--effort` | Reasoning effort for Claude/Codex; model-specific variant for OpenCode. |
| `--no-review` | Skip the review gate for this run without changing `romp.toml`. |
| `--width` | Concurrent jobs (ignored by `run`). |

`run` bypasses registry polling and trigger-label admission. The daemon records
an active row, logs, and an outcome, and rejects a duplicate local run of the
same issue. Ctrl-C requests cancellation.

## `romp watch`

Register this origin and view its daemon jobs. The daemon polls at `width`.

```bash
romp watch
```

On an interactive terminal, `watch` shows active jobs and recent history in a
full-screen dashboard. Each active job exposes its current agent, verification,
review, fix, or publication phase and the harness currently acting. History and
job details retain both builder and reviewer harness identities. Use Tab to
switch views, the arrow keys to navigate, Enter to inspect a job's phase
timeline, and Escape to return.

Successful verification output stays out of the job log. The log records the
command, duration, and result, while a failed command retains its output for
diagnosis. Non-interactive `watch` prints the registry binding and exits. Use
`romp logs ISSUE -f` to stream job output. q or Ctrl-C disconnects the dashboard
while the daemon keeps working.

The poll predicate is "has the trigger label and does not have the claim
label". One daemon polls all origins every 60 seconds. It admits at most one
new job per global tick while macOS memory pressure is normal and rotates the
first repository considered. Warning or critical pressure stops new admission.
Per-repo `width` still bounds concurrency.

## `romp status` and `romp history`

```bash
romp status            # in-flight jobs in this repo
romp status --all      # in-flight jobs across every repo on this machine
romp history           # recently finished jobs in this repo
romp history --all     # recently finished jobs across every repo
romp history --review --days 30
```

`status` shows each in-flight job's codename, issue, branch, elapsed time, and
optional `SESSION` identifier. `history` lists recently finished jobs with
their outcome, branch, PR URL, and optional `SESSION` identifier. A missing
session identifier appears as `-`.

`history --review` reports the clean-pass rate, fix-round rate, total
re-verification failures, and median review-pass wall duration over the selected
history window. One pass contains all concurrent lens calls, so the duration
tracks the slowest completed lens rather than the sum of reviewer compute. It
uses reviewed jobs as the rate denominator, so disabled and docs-only skips do
not dilute the rates.

## `romp logs`

```bash
romp logs sunny_naruto       # by codename
romp logs 17                 # or by issue number
romp logs sunny_naruto -f    # follow new lines
```

`logs` asks the daemon for bounded snapshots or a live JSON-RPC subscription.
The daemon must be running; the CLI does not read log files directly.

## `romp cancel`

```bash
romp cancel 17
```

Cancel asks the daemon to abandon the active job. Cancelling kills the agent, records a `cancelled` outcome, and removes
the claim label, the trigger label, the job row, and the worktree and `romp-N`
branch. The issue stays label-free until a human re-labels it.

## `romp gc`

```bash
romp gc                 # dry-run: print what it would remove
romp gc --apply         # actually delete
romp gc --history-days 14
```

`gc` removes stale worktrees whose job has no in-flight row, and prunes
finished-job history older than `history_days` (default 30, from user config).

| Flag | Description |
| --- | --- |
| `--apply` | Delete worktrees and history instead of listing them. |
| `--history-days` | Delete outcomes older than N days (0 disables; default from user config). |

## `romp doctor`

```bash
romp doctor
```

Checks the running daemon environment and prints a table:

- `git` — installed and 2.35+
- `gh` — installed and authenticated
- `builder` — the configured builder CLI is healthy
- `config` — `romp.toml` loads and a verify command is present
- `reviewer` — the effective reviewer CLI is healthy, or review is disabled
- executable paths and the installer-resolved environment

## `romp daemon`

```bash
romp daemon install --dry-run
romp daemon install                 # optionally --binary /absolute/path/rompd
romp daemon start
romp daemon status
romp daemon version
romp daemon restart                 # drains active jobs before restarting
romp daemon stop
```

Install resolves tools from the initiating user's login PATH and stages one
canonical binary at `~/Library/Application Support/romp/bin/rompd`. It writes
`~/Library/LaunchAgents/com.romp.daemon.plist` with RunAtLoad and KeepAlive.
Install refuses downgrades and does not interrupt active jobs. Reinstall when
program paths move, then restart to drain active jobs and load the staged binary.

Stop first blocks admission, requests cancellation, and waits for recorded
outcomes and cleanup. It then boots the agent out of the login session, so
KeepAlive does not restart it. A failed cleanup prevents a successful stop.

```bash
romp unwatch    # stop new claims for this origin; active work drains
romp rebind     # attach an existing origin to this checkout after a move
```

A second checkout of a watched origin is rejected. Missing paths remain in the
registry with an unhealthy status. Rebind requires the origin to match and its
jobs to be idle. All storage-backed commands require a running daemon.

## `romp init`

```bash
romp init
```

In an interactive terminal, inspects project configuration files and lets you
select or type one or more ordered verification commands. It then asks whether
to enable review, with yes as the default, before writing `romp.toml`. It
creates the three labels on the repo and appends `.romp/local.toml` to
`.gitignore`. If `romp.toml` already exists it is left untouched. In
non-interactive use, pass one or more repeatable `--verify` flags; Romp does not
guess commands and writes review as enabled.
