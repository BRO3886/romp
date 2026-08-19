---
title: "Configuration"
description: "romp.toml reference, config layering, and defaults."
weight: 4
---

Configuration is TOML. `romp init` writes a starting `romp.toml`:

```toml
label          = "romp"           # trigger label
claimed_label  = "romp:claimed"
blocked_label  = "romp:blocked"
base           = "main"           # default: repo default branch
width          = 3
timeout        = "25m"

[verify]
build = "go build ./..."
test  = "go test ./... -count=1"
lint  = "golangci-lint run"      # optional

[scope]
protected = ["testdata/**", "internal/testutil/**", ".github/**"]
ignore    = ["vendor/**", "node_modules/**"]

[harness]
default   = "codex"              # claude | codex | opencode
model     = ""
effort    = "high"               # claude: low..max; codex: + none, minimal, ultra; ignored by opencode
max_turns = 30                   # claude only; ignored by codex and opencode

[prompt]
template = ".romp/prompt.md"     # optional
brief    = ".romp/DESIGN.md"     # optional
```

## Precedence

Config merges in a fixed order — later layers override only the fields they
set:

```
flags  →  .romp/local.toml  →  romp.toml  →  ~/.config/romp/config.toml  →  defaults
```

Zero means "use the lower layer": `width = 0`, `base = ""`, an unset
`max_turns`, and empty strings all fall through. Commit `romp.toml` and
`.romp/*.md`; keep `local.toml` out of git (romp adds it to `.gitignore` for
you).

`history_days` is the one exception — it is read **only** from the user config
(`~/.config/romp/config.toml`), never from `romp.toml`. Retention is an
operator concern, not a team convention.

## Full reference

| Key | Default | Notes |
| --- | --- | --- |
| `label` | `romp` | The trigger label. |
| `claimed_label` | `romp:claimed` | Marks an issue as taken. |
| `blocked_label` | `romp:blocked` | Marks an under-scoped issue. |
| `base` | repo default branch | Branch worktrees fork from. |
| `width` | `3` | Max concurrent jobs in this repo. |
| `timeout` | `25m` | Per-job deadline. |
| `verify.build` | — | Build command. |
| `verify.test` | — | Test command. |
| `verify.lint` | — | Optional lint command. |
| `scope.protected` | — | Paths the agent must not touch. |
| `scope.ignore` | — | Paths the agent must not read. |
| `harness.default` | `codex` | `claude`, `codex`, or `opencode`. |
| `harness.model` | — | Specific model, or empty for the harness default. |
| `harness.effort` | `high` | Reasoning budget (see below). |
| `harness.max_turns` | — | Turn cap, claude only. |
| `prompt.template` | — | Custom goal-contract template. |
| `prompt.brief` | — | File the agent reads first (e.g. `.romp/DESIGN.md`). |
| `history_days` | `30` | Global only; outcome retention window. |

## Verify

romp refuses to run without a verify command — it never guesses. At least one
of `[verify]` `build`, `test`, or `lint` must be non-empty. romp re-runs every
non-empty verify command itself after the agent exits, in order, and opens the
PR only when all of them pass.

## Harness effort

The legal `effort` values depend on the harness. Shared names mean the same
thing and pass through unchanged. An invalid value fails at config load, not
inside the agent.

| Harness | Effort values |
| --- | --- |
| `claude` | `low`, `medium`, `high`, `xhigh`, `max` |
| `codex` | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra` |

## Where state lives

State lives outside the repo:

- `~/.local/state/romp/romp.db` — the shared job table (SQLite, one per machine)
- `~/.local/state/romp/<owner>-<repo>/logs/` — per-job logs
- Worktrees under the OS cache dir (`~/Library/Caches/romp/<owner>-<repo>/romp-N` on macOS)
