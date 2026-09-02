---
title: "Getting Started"
description: "Install romp, verify your machine, and run your first labelled issue."
weight: 2
---

## Requirements

- `git` 2.35+ — the first release with reliable worktree support
- `gh` — authenticated via `gh auth login`
- `codex`, `claude`, or `opencode`, logged in
- Go 1.25+ to build, or a release binary

## Install

### Go install (recommended)

```bash
go install github.com/BRO3886/romp/cmd/romp@latest
```

### Download a release

Apple Silicon:

```bash
curl -LO https://github.com/BRO3886/romp/releases/latest/download/romp-darwin-arm64.tar.gz
tar xzf romp-darwin-arm64.tar.gz
mkdir -p ~/.local/bin && mv romp ~/.local/bin/romp
```

Intel:

```bash
curl -LO https://github.com/BRO3886/romp/releases/latest/download/romp-darwin-amd64.tar.gz
tar xzf romp-darwin-amd64.tar.gz
mkdir -p ~/.local/bin && mv romp ~/.local/bin/romp
```

### Build from source

```bash
git clone https://github.com/BRO3886/romp.git
cd romp
make build
# Binary at ./bin/romp
```

## Install the agent skill

The release binary includes `rompify`, an agent skill that converts short
requests and existing GitHub issues into repository-grounded execution
contracts. Install it into an agent's skill directory:

```bash
romp skills install --agent codex
```

Use `--agent claude`, `--agent openclaw`, or `--agent all` for other targets.
Add `--dry-run` to preview the exact files. The skill becomes available in the
next agent session.

## Quickstart

romp reads the repo from your current directory's `origin` remote.

```bash
cd ~/code/your-project

romp doctor        # verify git, gh, harness, config
romp init          # write romp.toml, create labels, update .gitignore
romp run -i 17     # run one issue in the foreground
romp watch         # then let it run
```

In an interactive terminal, `init` inspects project files and shows candidate
commands. Select or type one or more commands in the order Romp should run
them. The candidates are suggestions only; Romp does not validate or execute
them during initialization.

| Source | Example candidates |
| --- | --- |
| `Makefile` | `make test`, `make lint` |
| `package.json` | `npm test`, `npm run lint` |
| `go.mod` | `go test ./...` |
| `Cargo.toml` | `cargo test` |
| `pyproject.toml` | `pytest` |

GitHub Actions workflow files are not discovery sources because their `run`
steps can depend on CI orchestration, containers, or environment-specific
setup. Add those commands explicitly if they are also valid for local
verification.

The resulting configuration uses an ordered `[verify].commands` list:

```toml
[verify]
commands = ["make test", "make lint"]
```

For scripts, pass repeatable flags instead. Non-interactive initialization does
not guess commands:

```bash
romp init --verify "make test" --verify "make lint"
```

`init` also creates three labels — the trigger label (`romp`), the claim label
(`romp:claimed`), and the blocked label (`romp:blocked`) — and appends
`.romp/local.toml` to your `.gitignore`.

## Watch it run

On an interactive terminal, `watch` opens a full-screen dashboard:

```text
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

Each active job shows the phase it is in, the harness acting right now, and how
long it has been running. `CODEX → CLAUDE` reads as builder then reviewer. Tab
switches between active jobs and history, the arrow keys move the selection, and
Enter opens the selected job's phase timeline.

When stdout is not a terminal, `watch` falls back to line-oriented output for
scripts and service managers:

```text
watching label "romp" every 1m0s (width 3)
[sunny_naruto] running codex
[sunny_naruto] verify ok (go test ./... -count=1)
[sunny_naruto] PR: https://github.com/you/your-project/pull/482
#17: done
```

`watch` polls every 60 seconds and works up to `width` issues at once. Ctrl-C
drains — it stops claiming and waits for running jobs to finish. Ctrl-C twice
kills everything.

## Next steps

- [Commands](/docs/commands/) — every command and flag
- [Configuration](/docs/configuration/) — tune `romp.toml`
- [Goal Contract](/docs/goal-contract/) — what romp tells your agent
