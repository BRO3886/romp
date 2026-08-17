# romp

**Label an issue. Get a pull request.**

`romp` watches a GitHub repo for issues carrying a label, runs your local coding agent on each one in an isolated git worktree, verifies the result against your test suite, and opens a PR.

It runs on your machine and uses your existing Claude Code or Codex login. No API keys, no cloud runner, no new billing relationship.

> A *romp* is a group of otters. Otters use rocks to crack shellfish — small tool users solving scoped problems. That is the whole idea.

---

## Status

**Pre-alpha.** The interface below is the target, not a promise. Expect breaking changes. Do not point this at a repo you cannot afford to have a bad branch pushed to.

---

## Requirements

- Go 1.22+ (to build) or a release binary
- `git` 2.35+ (worktree support)
- [`gh`](https://cli.github.com), authenticated (`gh auth login`)
- One agent CLI, logged in:
  - [Claude Code](https://claude.com/claude-code) — `claude`
  - [Codex](https://developers.openai.com/codex) — `codex`

---

## Install

```bash
go install github.com/BRO3886/romp/cmd/romp@latest
```

Or grab a binary from [Releases](../../releases).

---

## Quickstart

`romp` works on the repo you are standing in. It finds the git root from your
current directory and reads the repo from your `origin` remote.

```bash
cd ~/code/your-project

romp init          # writes romp.toml, creates the label on GitHub
romp run -i 17     # try one issue first
romp watch         # then let it run
```

```
$ romp watch
romp v0.1.0 — your-org/your-project (main), width 3, harness claude
watching label "romp" every 60s

19:04:12  assigned issue #17 to agent  [job 1a4f]
19:04:12  worktree ~/.cache/romp/your-project/romp-17
19:22:38  #17 verify: go test ./... -count=1 → ok
19:22:44  #17 → PR #482 https://github.com/your-org/your-project/pull/482

19:31:05  assigned issue #23 to agent  [job 7c02]
19:38:19  #23 blocked: acceptance criteria contradict each other → needs-scoping
```

Stop with Ctrl-C. Running jobs finish; no new ones start. `Ctrl-C` twice kills
everything.

---

## Commands

All commands operate on the repo containing your current directory.

| Command | What it does |
|---|---|
| `romp init` | Write `romp.toml`, create the label on GitHub, update `.gitignore`. |
| `romp watch` | Poll for labelled issues and work them. Foreground. |
| `romp run -i N` | Run one issue now. Ignores the label. |
| `romp status` | Jobs in this repo. `--all` for every running instance. |
| `romp cancel <id>` | Kill a running job and free its slot. |
| `romp logs <id> [-f]` | Show or follow a job's log. |
| `romp gc` | Remove worktrees left behind by failed jobs. |
| `romp doctor` | Check `gh` auth, harness login, git version, config validity. |

`status`, `cancel`, and `logs` reach the watcher over a Unix socket at
`$XDG_RUNTIME_DIR/romp/<owner>-<repo>.sock`. They work without a watcher running
too — they fall back to reading the job table directly.

Run `romp doctor` first. It catches the three things that break every new setup:
`gh` not authenticated, the agent CLI not logged in, and git older than 2.35.

---

## How it works

```
issue labelled "romp"
        │
        ▼
  claim a slot          ← SQLite job table, width N
        │
        ▼
  git worktree add      ← fresh branch off the default branch
        │
        ▼
  render goal prompt    ← from the issue body + your template
        │
        ▼
  run the harness       ← claude -p  |  codex exec
        │
        ▼
  verify independently  ← romp re-runs your test command itself
        │
        ▼
  push + open PR        ← label removed, worktree cleaned up
```

Every job gets its own worktree, its own branch, and its own log file. Jobs run in parallel up to the configured width.

**The verification step is separate on purpose.** The agent's own claim that tests pass is not proof — `romp` runs your test command itself, in the worktree, after the agent exits. No green, no PR.

---

## Configuration

Config is per repo. `romp init` writes `romp.toml` at the repo root:

```toml
label   = "romp"                     # trigger label
base    = "main"                     # default: repo default branch
width   = 3                          # concurrent jobs in this repo
timeout = "25m"

[verify]
build = "go build ./..."
test  = "go test ./... -count=1"
lint  = "golangci-lint run"          # optional

[scope]
protected = ["testdata/**", "internal/testutil/**", ".github/**"]
ignore    = ["vendor/**", "node_modules/**"]

[harness]
default   = "claude"                 # claude | codex
model     = ""                       # harness default if empty
max_turns = 30                       # claude only; ignored for codex

[prompt]
template = ".romp/prompt.md"         # optional; built-in default if absent
brief    = ".romp/DESIGN.md"         # optional; READ FIRST context for the agent
```

`repo` is not a key. It comes from your `origin` remote.

### Where things live

```
your-project/
  romp.toml            # committed — team settings: label, verify, scope
  .romp/
    prompt.md          # committed — your goal-prompt template
    DESIGN.md          # committed — briefing the agent reads first
    local.toml         # gitignored — your machine: harness, width, model
```

Commit `romp.toml` and `.romp/*.md`. They are how the repo tells any contributor's
agent what "done" means here. Keep `local.toml` out of git — your teammate may
run Codex at width 1 while you run Claude at width 5.

`romp init` adds `.romp/local.toml` to `.gitignore` for you.

### Precedence

Highest wins:

```
command flags
  → .romp/local.toml        (your machine, this repo)
    → romp.toml             (this repo, committed)
      → ~/.config/romp/config.toml   (your defaults across all repos)
        → built-in defaults
```

So `romp run -i 17 --harness codex --width 1` overrides everything for one run.

### Multiple repos

`romp watch` watches one repo — the one you are in. To run several, start one
per repo; each gets its own socket and its own job table under
`~/.local/state/romp/<owner>-<repo>/`.

`romp status --all` shows jobs across every running instance.

---

## The goal prompt

`romp` does not just hand the issue body to the agent. It renders a goal contract — a verifiable finish line, not a task description:

- **DONE** — the acceptance criteria from the issue, on a clean tree.
- **PROVE IT** — the exact commands whose fresh output must appear before the agent stops.
- **CONSTRAINTS** — what must not be touched to get there: no deleted tests, no weakened assertions, no skips, no hardcoded expected values, no out-of-scope files.

Failing tests are not a stopping condition. The agent fixes and re-runs.

Override the template at `.romp/prompt.md`, in the repo. Placeholders:
`{{ISSUE}}` `{{TITLE}}` `{{BODY}}` `{{REPO}}` `{{BRANCH}}` `{{BASE}}` `{{URL}}`
`{{VERIFY}}` `{{PROTECTED}}`.

Put anything too long for a goal condition in `.romp/DESIGN.md` — repo idioms,
architecture notes, where the test fixtures live. `romp` passes it as a
`READ FIRST` pointer so the agent loads the briefing before touching code.

---

## Writing issues romp can work

`romp` assumes the issue is **fully scoped** — work that needs implementation, not deliberation. Good issues have:

- Acceptance criteria a test could check
- The files or area expected to change
- What is explicitly out of scope

If the agent finds the issue ambiguous or contradictory, it stops without writing code, and `romp` relabels the issue `needs-scoping` and posts the specific gap as a comment. That signal is the point. An under-scoped issue produces a plausible PR that solves the wrong problem, which costs more than no PR at all.

---

## Job outcomes

| Outcome | What you get |
|---|---|
| **done** | PR opened, label removed, worktree removed |
| **blocked** | No PR. Issue relabelled `needs-scoping`, gap posted as a comment |
| **no-changes** | Agent exited clean but produced no commits. No PR, job failed |
| **red** | Agent finished, `test_cmd` failed on the independent re-run. No PR, worktree kept |
| **timeout** | Job exceeded `timeout`. Killed, worktree kept for inspection |
| **rate-limited** | Requeued with backoff, up to 3 attempts |

Failed jobs keep their worktree so you can inspect it. `romp gc` removes ones older than a week.

---

## Safety

Read this before running `romp watch` unattended.

- **The agent runs with tool access on your machine, driven by text that anyone with write access to the repo can author.** Treat the trigger label as a privileged permission. Restrict who can apply it.
- **`romp` never merges.** It opens PRs. Review them.
- **Do not run it against a repo with production credentials in the working tree.**
- Branch protection on your default branch is strongly recommended.
- Agent runs are sandboxed only as far as your harness sandboxes them. `codex exec --sandbox workspace-write` is the safer default; Claude's `--dangerously-skip-permissions` is not a sandbox.

---

## Non-goals

Things `romp` will not do, so you know what to reach for instead:

- **Interactive sessions.** Use the agent CLI directly, or [T3 Code](https://github.com/pingdotgg/t3code).
- **Streaming or live steering.** `romp` is batch. The PR is the output.
- **Cloud execution.** Use Codex Cloud or the Claude Code GitHub Action.
- **Merging, deploying, or anything past the PR.**
- **Being a general agent framework.** It does one thing.

---

## Contributing

Harness adapters live in `internal/harness/`. Each implements a small interface and declares which features it supports — an adapter that does not support a feature says so explicitly rather than silently ignoring it.

Tests run offline against a fake harness and a fixture repo. `make test` needs no network, no GitHub, and no agent installed.

---

## License

MIT
