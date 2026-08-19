# romp

![romp](.github/romp.png)

**Label an issue. Get a pull request.**

`romp` watches a GitHub repo for labelled issues, runs your local coding agent on each in an isolated git worktree, independently re-runs your test commands, and opens a PR. It uses your existing Claude Code, Codex, or OpenCode login — no API keys, no cloud runner.

> **Pre-alpha.** Breaking changes expected. Do not point this at a repo you can't afford a bad branch on.

## Requirements

- `git` 2.35+, `gh` (authenticated via `gh auth login`)
- `codex`, `claude`, or `opencode`, logged in
- Go 1.25+ to build, or a release binary

## Install

```bash
go install github.com/BRO3886/romp/cmd/romp@latest
```

## Quickstart

`romp` reads the repo from your current directory's `origin` remote.

```bash
cd ~/code/your-project

romp doctor        # verify git, gh, harness, config
romp init          # write romp.toml, create labels, update .gitignore
romp run -i 17     # run one issue in the foreground
romp watch         # then let it run
```

`init` detects the language (go, rust, node, python, Makefile) and seeds the
verify command. If nothing is detected, add one to `romp.toml` yourself.

```
$ romp watch
19:04:12  watching label "romp" every 1m0s (width 3)
19:04:12  [sunny_naruto] running codex
19:22:38  [sunny_naruto] verify ok (go test ./... -count=1)
19:22:44  [sunny_naruto] PR: https://github.com/you/your-project/pull/482
19:22:44  #17: done
```

Ctrl-C drains; twice kills.

## Commands

| Command | What it does |
|---|---|
| `init` | Write `romp.toml`, create the labels, update `.gitignore`. |
| `watch` | Poll and work labelled issues. One repo, foreground. |
| `run -i N` | Run one issue now, ignoring the label. |
| `status` | In-flight jobs. `--all` for every repo. |
| `history` | Recently finished jobs. `--all` for every repo. |
| `logs <codename\|issue> [-f]` | Show or follow a job's log. |
| `cancel <issue>` | Kill a running job and abandon it. |
| `gc` | Remove stale worktrees and old history. Dry-run unless `--apply`. |
| `doctor` | Check git, gh auth, harness, config. |

`run` takes `--verify`, `--harness`, `--model`, `--effort` to override config.

`cancel` talks to the live watcher over a socket — no watcher, nothing to
cancel. `logs` reads log files directly and works either way.

## Configuration

`romp init` writes `romp.toml`:

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

Precedence: flags → `.romp/local.toml` (gitignored) → `romp.toml` →
`~/.config/romp/config.toml` → defaults. Commit `romp.toml` and `.romp/*.md`;
keep `local.toml` out of git. `history_days` is global-only (user config).

State lives outside the repo: `~/.local/state/romp/romp.db` (shared job table),
`~/.local/state/romp/<owner>-<repo>/logs/`, and worktrees under the cache dir.

## The goal contract

`romp` hands the agent a finish line, not a task description:

- **GATE** — reject the issue before touching code if it's ambiguous,
  contradictory, or under-scoped. Don't invent missing criteria.
- **DONE** — every acceptance criterion met, on a clean tree.
- **PROVE IT** — run the verify command and show the fresh passing output.
- **CONSTRAINTS** — no deleted/skipped/weakened tests, no hardcoded expected
  values, no out-of-scope files, no `git commit`/`git push`.

The agent reports in `.romp/pull-request.md` (PR title + conventional-commit
subject + description) or `.romp/blocked.md` (the specific gap). Customize the
template at `.romp/prompt.md`; put long repo context in `.romp/DESIGN.md`.

An under-scoped issue gets relabelled `romp:blocked` with the gap posted as a
comment — a plausible PR solving the wrong problem costs more than no PR.

## Outcomes

| Outcome | What you get |
|---|---|
| **done** | PR opened, trigger label removed. |
| **blocked** | No PR. `romp:blocked` label + gap comment. |
| **no-changes** | Agent exited clean with no commits. No PR. |
| **red** | Verify failed on independent re-run. No PR, worktree kept. |
| **timeout** | Exceeded `timeout`. Killed, worktree kept. |
| **cancelled** | You cancelled. Worktree, branch, and both labels removed. |
| **error** | git/gh failure (incl. rate limits outliving retries). Re-claimed later. |

On claim, romp adds the claim label and assigns the authenticated user; both
clear when the job ends. `cancel` is an abandon: it removes the trigger label
too, so re-label the issue to retry it. `gc` reclaims leftover worktrees.

## Safety

- The agent runs tools on your machine, driven by text anyone with repo write
  access can author. Treat the trigger label as privileged.
- romp never merges. Review the PRs.
- Don't run it against a repo with production credentials in the tree.
- Branch protection on the default branch is strongly recommended.
- Sandboxing is whatever the harness provides: Codex runs `--sandbox
  workspace-write`, Claude `--permission-mode bypassPermissions`, and OpenCode
  runs `--dangerously-skip-permissions`.

## Non-goals

Interactive sessions, live steering, cloud execution, merging/deploying, or
being a general agent framework. Use the agent CLI, T3 Code, or Codex Cloud for
those.

## Contributing

Harness adapters live in `internal/harness/`. `make test` runs offline against
a fake harness and fixture repo — no network, no GitHub, no agent.

## License

MIT
