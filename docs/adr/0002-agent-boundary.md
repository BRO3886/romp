# The agent boundary: a prompt contract, outcomes via .romp/ files

Status: accepted

## Context

romp supports multiple coding-agent CLIs — claude and codex — and each ships its own structured-output mechanism (claude's `--json-schema`, Codex's `--output-schema`). Coupling romp to any one of them means learning a new output format per harness, and the agent's final free-text message is not a reliable carrier for structured data.

## Decision

The prompt is the single contract between romp and the agent. The agent reports structured outcomes by writing markdown files under `.romp/` — `pull-request.md` (PR title, conventional commit subject, description, optionally mermaid diagrams) and `blocked.md` (the gap when an issue is under-scoped) — which romp reads after the harness exits. The harness interface stays minimal (`Name` plus `Run`); adapters implement it, and romp never parses a harness's native output.

## Consequences

- A new harness drops in without romp learning its output format; the `.romp/` convention is harness-agnostic.
- Outcome artifacts must not reach the committed diff: romp removes `pull-request.md` before committing, and `blocked.md` is consumed on a path that returns before any commit.
- The contract is only as strong as the agent's obedience to it, so romp falls back to defaults (issue title, conventional commit, "Closes #N") when the artifact is missing or malformed.
