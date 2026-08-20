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

```bash
$ romp watch
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
