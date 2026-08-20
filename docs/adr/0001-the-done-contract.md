# The done contract: an explicit verify command, independently re-run

Status: accepted

## Context

romp's only value is that a green PR actually solves the issue. Two things threaten that. First, the agent may report tests pass without running them, so trusting its claim means the cheapest green wins. Second, romp has no a priori knowledge of how a given repo proves "done" — Go, Rust, JavaScript, and Python each use a different command.

## Decision

A job is done only when an explicit verification command passes, run by romp itself after the agent exits — the agent's own claim is never trusted. The commands are a per-repo contract committed in `romp.toml` under `[verify].commands`, and romp refuses to run without them rather than guessing. `romp init` discovers candidate commands from project files and lets the user choose an ordered list; discovery does not validate or execute the candidates. See ADR 0011.

## Consequences

- Both failure modes are removed: the command cannot be wrong silently, and the green cannot be unverified.
- Runtime language detection is deliberately rejected. Monorepos and custom runners make any heuristic wrong often enough to matter, and a silent wrong guess is worse than failing loudly — a wrong test command produces a plausible PR that solves the wrong problem, which costs more than no PR.
- The cost is friction: a repo without `[verify]` cannot be run until `init` or a `--verify` flag supplies the command.
