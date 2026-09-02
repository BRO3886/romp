# romp

![romp](.github/romp.png)

**Label an issue. Get a pull request.**

`romp` is an opinionated runner for the coding agent you already have. It watches a GitHub repo for labelled issues, runs your agent on each in an isolated git worktree, independently re-runs your test commands, reviews the diff before you do, and opens a PR. It uses your existing Claude Code, Codex, or OpenCode login — no API keys, no cloud runner.

Opinionated means romp makes these calls for you, and does not offer a switch to unmake them:

- **The agent gets a finish line, not a task.** Every job is a goal contract — GATE, DONE, PROVE IT, CONSTRAINTS — and an under-scoped issue is rejected before a single file is edited.
- **The agent's word is never evidence.** romp re-runs your verify commands itself, in the worktree, after the agent exits.
- **Every diff is reviewed before you see it.** A read-only review gate fans out across lenses picked from the changed files, and blocking findings go back to the builder as constraints.
- **romp never merges, and never asks to.** It opens the PR and stops.
- **Your machine, your login, your agent.** No API keys, no cloud runner, no hosted queue.

> **Pre-alpha.** Breaking changes expected. Do not point this at a repo you can't afford a bad branch on.

## Requirements

- `git` 2.35+, `gh` (authenticated via `gh auth login`)
- `codex`, `claude`, or `opencode`, logged in
- Go 1.25+ to build, or a release binary

## Install

```bash
go install github.com/BRO3886/romp/cmd/romp@latest
```

Romp includes the `rompify` agent skill. Install it for a detected agent, or
select one explicitly:

```bash
romp skills install --agent codex
romp skills install --agent claude
romp skills install --agent codex --dry-run
```

The skill turns short requests and existing GitHub issues into deterministic,
repository-grounded execution contracts. It does not implement the issue.

## Quickstart

`romp` reads the repo from your current directory's `origin` remote.

```bash
cd ~/code/your-project

romp doctor        # verify git, gh, harness, config
romp init          # choose verification commands, then write romp.toml
romp run -i 17     # run one issue in the foreground
romp watch         # then let it run
```

`init` reads local project configuration and manifest files and shows discovered
commands from Makefiles, package manifests, and language manifests. It does not
use GitHub Actions workflow commands as local verification candidates. Select
commands or type your own. Use repeatable `--verify` flags in non-interactive
environments.

In an interactive terminal, `init` shows the selected commands and a filtered
candidate list. Enter adds the highlighted or typed command. Submit an empty
input to finish. The order in which you add commands is the order Romp uses.

```text
Verification commands
Selected commands:
  1. make test

verify> make l▌
  make lint                         Makefile target "lint"
  npm --prefix frontend run lint    frontend/package.json script "lint"
```

For automation, pass the commands explicitly:

```bash
romp init --verify "make test" --verify "make lint"
```

```
$ romp watch
ROMP  you/your-project  2 active  ● WATCHING

Active 2   History 9

╭──────────────────────────────────────────────────────────────╮
│ ▸ #17  ◈ REVIEWING  CODEX → CLAUDE  18m32s                    │
│    Synchronize relay device readiness and sender identities   │
│    reviewer working across 6 lenses (read-only)               │
│                                                              │
│   #21  ◆ AGENT  CLAUDE  4m08s                                 │
│    Retry a failed review verification within budget           │
│    agent working                                              │
╰──────────────────────────────────────────────────────────────╯

tab switch   ↑/↓ navigate   enter inspect   q drain
```

The interactive dashboard shows each job moving through agent, verification,
review, fix, and publication phases. Tab switches between active jobs and
history. Active jobs identify the harness currently acting, while history and
details retain both builder and reviewer identities. Enter opens the selected
job's phase timeline. Successful verification output is not copied into the job
log; failures retain their diagnostic output and show any transition into a
remaining repair round. A reviewer-start line is emitted immediately before
each concurrent read-only lens fan-out begins.
When stdout is not a terminal, `watch` keeps the line-oriented output used by
scripts and service managers. Ctrl-C drains; twice kills.

## Commands

| Command | What it does |
|---|---|
| `init` | Write `romp.toml`, create the labels, update `.gitignore`. |
| `watch` | Open the interactive job dashboard and work labelled issues. |
| `run -i N` | Run one issue now, ignoring the label. |
| `status` | In-flight jobs. `--all` for every repo. |
| `history` | Recently finished jobs. `--all` for every repo. `--review` for reviewer calibration. |
| `logs <codename\|issue> [-f]` | Show or follow a job's log. |
| `cancel <issue>` | Kill a running job and abandon it. |
| `gc` | Remove stale worktrees and old history. Dry-run unless `--apply`. |
| `doctor` | Check git, gh auth, harness, config. |
| `skills install\|uninstall\|status` | Manage the bundled `rompify` agent skill. |

`run` takes repeatable `--verify`, `--harness`, `--model`, and `--effort` flags to override config.
For OpenCode, `--effort` selects the model-specific `--variant` value.

`cancel` talks to the live watcher over a socket — no watcher, nothing to
cancel. `logs` reads log files directly and works either way.

## Configuration

`romp init` writes `romp.toml`:

```toml
label          = "romp"           # trigger label
claimed_label  = "romp:claimed"
blocked_label  = "romp:blocked"
changes_requested_label = "romp:changes-requested"
base           = "main"           # default: repo default branch
width          = 3

[verify]
commands = [
  "go build ./...",
  "go test ./... -count=1",
  "golangci-lint run",
]

[scope]
protected = ["testdata/**", "internal/testutil/**", ".github/**"]
ignore    = ["vendor/**", "node_modules/**"]

[harness]
default   = "codex"              # claude | codex | opencode
model     = ""
effort    = "high"               # claude/codex: reasoning effort; opencode: model-specific variant
max_turns = 30                   # claude only; ignored by codex and opencode

[review]
enabled        = true
model          = ""             # missing or empty uses harness.model
harness        = ""             # missing or empty uses harness.default
max_fix_rounds = 2              # each round includes build, verify, push, and review

[prompt]
template = ".romp/prompt.md"     # optional
brief    = ".romp/DESIGN.md"     # optional
```

For OpenCode, `harness.effort` is passed as `opencode run --variant`. Variant
names are model-specific, so Romp prints one warning at startup when an
effective variant comes from configuration. A command-line `--effort` override
does not produce this configuration warning.

Precedence: flags → `.romp/local.toml` (gitignored) → `romp.toml` →
`~/.config/romp/config.toml` → defaults. Commit `romp.toml` and `.romp/*.md`;
keep `local.toml` out of git. `history_days` is global-only (user config).

Omit `timeout` to let jobs run without a deadline. Set it to a Go duration
string such as `25m`, `1.5h`, or `2h45m` when a job needs a deadline.
The deadline covers the builder, verification, review, and all configured fix
rounds as one job budget.

State lives outside the repo: `~/.local/state/romp/romp.db` (shared job table),
`~/.local/state/romp/<owner>-<repo>/logs/`, and worktrees under the cache dir.

## The goal contract

`romp` hands the agent a finish line, not a task description:

- **GATE** — reject the issue before touching code if it's ambiguous,
  contradictory, or under-scoped. Don't invent missing criteria.
- **DONE** — every acceptance criterion met, on a clean tree.
- **PROVE IT** — run the verification commands and show the fresh passing output.
- **CONSTRAINTS** — no deleted/skipped/weakened tests, no hardcoded expected
  values, no out-of-scope files, no `git commit`/`git push`.

The agent reports in `.romp/pull-request.md` (PR title + conventional-commit
subject + description) or `.romp/blocked.md` (the specific gap). Romp appends
its attribution after the description and issue-closing reference. Customize
the template at `.romp/prompt.md`; put long repo context in `.romp/DESIGN.md`.

An under-scoped issue gets relabelled `romp:blocked` with the gap posted as a
comment — a plausible PR solving the wrong problem costs more than no PR.

## Outcomes

| Outcome | What you get |
|---|---|
| **done** | PR opened, trigger label removed. |
| **blocked** | No PR. `romp:blocked` label + gap comment. |
| **no-changes** | Agent exited clean with no commits. No PR. |
| **changes-requested** | Verify passed, but blocking review findings remained after the configured fix rounds. PR open with every review-pass comment, worktree kept. |
| **red** | Independent verification failed with no repair round remaining. A PR may already exist after review. Trigger label removed and worktree kept. Watch jobs retain failed output in `romp logs ISSUE`; one-shot runs print it to stderr. |
| **timeout** | Exceeded `timeout`. Killed, trigger label removed, worktree kept. |
| **cancelled** | You cancelled. Worktree, branch, and both labels removed. |
| **error** | git/gh failure (incl. rate limits outliving retries). Trigger label removed, worktree kept. |

On claim, romp adds the claim label and assigns the authenticated user; both
clear when the job ends. Failed jobs also remove the trigger label so the same
failure is not repeated on the next poll. `cancel` is an abandon: it removes
the trigger label too, so re-label the issue to retry it. `gc` reclaims leftover
worktrees.

## Safety

- The agent runs tools on your machine, driven by text anyone with repo write
  access can author. Treat the trigger label as privileged.
- romp never merges. Review the PRs.
- Don't run it against a repo with production credentials in the tree.
- Branch protection on the default branch is strongly recommended.
- Sandboxing is whatever the harness provides: Codex runs `--sandbox
  workspace-write`, Claude `--permission-mode bypassPermissions`, and OpenCode
  runs `--auto` to auto-approve permissions that are not explicitly denied.

## Non-goals

Interactive agent sessions, live steering, cloud execution, merging/deploying,
or being a general agent framework. Use the agent CLI, T3 Code, or Codex Cloud for
those.

## Contributing

Harness adapters live in `internal/harness/`. `make test` runs offline against
a fake harness and fixture repo — no network, no GitHub, no agent.

## License

MIT
