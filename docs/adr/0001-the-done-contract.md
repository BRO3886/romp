# The done contract: an explicit verify command, independently re-run

Status: accepted

## Context

romp's only value is that a green PR actually solves the issue. Two things threaten that. First, the agent may report tests pass without running them, so trusting its claim means the cheapest green wins. Second, romp has no a priori knowledge of how a given repo proves "done" — Go, Rust, JavaScript, and Python each use a different command.

## Decision

A job is done only when an explicit verification command passes, run by romp itself after the agent exits — the agent's own claim is never trusted. The command is a per-repo contract committed in `romp.toml` under `[verify]`, and romp refuses to run without it rather than guessing. `romp init` seeds the command by detecting the language once (`go.mod` → `go test ./... -count=1`, `Cargo.toml` → `cargo test`, `package.json` → `npm test`, `pyproject.toml` → `pytest`, `Makefile` → `make test`), and the human confirms it before committing.

## Consequences

- Both failure modes are removed: the command cannot be wrong silently, and the green cannot be unverified.
- Runtime language detection is deliberately rejected. Monorepos and custom runners make any heuristic wrong often enough to matter, and a silent wrong guess is worse than failing loudly — a wrong test command produces a plausible PR that solves the wrong problem, which costs more than no PR.
- The cost is friction: a repo without `[verify]` cannot be run until `init` or a `--verify` flag supplies the command.
