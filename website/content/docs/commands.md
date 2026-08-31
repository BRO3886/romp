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
| `romp watch` | Poll and work labelled issues. One repo, foreground. |
| `romp run -i N` | Run one issue now, ignoring the label. |
| `romp status` | In-flight jobs. `--all` for every repo. |
| `romp history` | Recently finished jobs. `--all` for every repo. |
| `romp logs <codename\|issue> [-f]` | Show or follow a job's log. |
| `romp cancel <issue>` | Kill a running job and abandon it. |
| `romp gc` | Remove stale worktrees and old history. Dry-run unless `--apply`. |
| `romp doctor` | Check git, gh auth, harness, config. |

## `romp run`

Run one issue now, in the foreground, without claiming it in the job table.

```bash
romp run -i 17
```

| Flag | Description |
| --- | --- |
| `-i, --issue` | Issue number to run (required). |
| `--verify` | Command that must pass in the worktree before a PR opens (overrides config). |
| `--harness` | Harness to use (`claude`, `codex`, or `opencode`). |
| `--model` | Model for the harness. |
| `--effort` | Reasoning effort for Claude/Codex; model-specific variant for OpenCode. |
| `--no-review` | Skip the review gate for this run without changing `romp.toml`. |
| `--width` | Concurrent jobs (ignored by `run`). |

`run` is a pure foreground bypass: no job row, no dedupe, absent from `status`,
no log file. It only cleans its own stale `romp-N` branch and worktree.

## `romp watch`

Poll for trigger-labelled issues and work them at `width`.

```bash
romp watch
```

The poll predicate is "has the trigger label and does not have the claim
label". The poll interval is fixed at 60 seconds.

## `romp status` and `romp history`

```bash
romp status            # in-flight jobs in this repo
romp status --all      # in-flight jobs across every repo on this machine
romp history           # recently finished jobs in this repo
romp history --all     # recently finished jobs across every repo
```

`status` shows each in-flight job's codename, issue, branch, elapsed time, and
optional `SESSION` identifier. `history` lists recently finished jobs with
their outcome, branch, PR URL, and optional `SESSION` identifier. A missing
session identifier appears as `-`.

## `romp logs`

```bash
romp logs sunny_naruto       # by codename
romp logs 17                 # or by issue number
romp logs sunny_naruto -f    # follow new lines
```

`logs` reads the per-job log file directly, so it works with no watcher
running.

## `romp cancel`

```bash
romp cancel 17
```

Cancel talks to the live watcher over a Unix socket — no watcher, nothing to
cancel. Cancelling kills the agent, records a `cancelled` outcome, and removes
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

Checks five things and prints a table:

- `git` — installed and 2.35+
- `gh` — installed and authenticated
- `harness` — at least one of `claude`, `codex`, or `opencode` is healthy
- `config` — `romp.toml` loads and a verify command is present
- `review` — the effective reviewer CLI is healthy, or review is disabled

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
