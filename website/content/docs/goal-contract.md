---
title: "Goal Contract"
description: "The finish line romp hands your agent — GATE, DONE, PROVE IT, CONSTRAINTS."
weight: 5
---

romp hands the agent a finish line, not a task description. The rendered prompt
is the single contract between romp and the agent, and it is harness-agnostic:
Claude, Codex, and OpenCode receive the same structure.

## The four sections

- **GATE** — reject the issue before touching code if it's ambiguous,
  contradictory, or under-scoped. Don't invent missing criteria.
- **DONE** — every acceptance criterion met, on a clean tree.
- **PROVE IT** — run every configured verification command in order and show the fresh passing output for each.
- **CONSTRAINTS** — no deleted, skipped, or weakened tests, no hardcoded
  expected values, no out-of-scope files, no `git commit`/`git push`.

The GATE is the point. An issue is shippable only if all three of these are
already written in the issue body:

1. At least one acceptance criterion a test could check.
2. The files or area expected to change.
3. Enough constraint that two competent agents would ship the same product.

If any is missing — or the issue is a menu of options with no choice made —
the agent must self-reject before editing source, write the specific gap to
`.romp/blocked.md`, and stop. A plausible PR solving the wrong problem costs
more than no PR.

## Outcome artifacts

The agent reports a structured result by writing a markdown file under
`.romp/`:

- `pull-request.md` — PR title, conventional-commit subject, and description
  (with mermaid diagrams for substantial changes)
- `blocked.md` — the specific gap when an issue is under-scoped

romp reads these after the harness exits and never parses a harness's native
output. `pull-request.md` is removed before committing; `blocked.md` is
consumed on a path that returns before any commit, so neither reaches the diff.

When the artifact is missing or malformed, romp falls back to safe defaults:
the issue title, a conventional commit, and `Closes #N`.

## Customizing the prompt

Override the built-in template with `.romp/prompt.md` (a Go `text/template`,
configured via `[prompt] template`). Put long repo context — architecture,
conventions, invariants — in `.romp/DESIGN.md` and point `[prompt] brief` at it;
the agent is told to read it first.

When `blocked`, romp relabels the issue `romp:blocked` and posts the gap as a
comment, so the human knows exactly what to add to make the issue shippable.
