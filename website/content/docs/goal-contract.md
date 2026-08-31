---
title: "Goal Contract"
description: "The finish line romp hands your agent — GATE, DONE, PROVE IT, CONSTRAINTS."
weight: 5
---

romp hands the builder a finish line, not a task description. The rendered
builder prompt is the contract between romp and the builder, and it is
harness-agnostic: Claude, Codex, and OpenCode receive the same structure.

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

## Builder outcome artifacts

The builder reports a structured result by writing a markdown file under
`.romp/`:

- `pull-request.md` — PR title, conventional-commit subject, and description
  (with mermaid diagrams for substantial changes)
- `blocked.md` — the specific gap when an issue is under-scoped

romp reads these builder artifacts after the harness exits. It does not parse
the builder's native output. `pull-request.md` is removed before committing;
`blocked.md` is consumed on a path that returns before any commit, so neither
reaches the diff.

When the artifact is missing or malformed, romp falls back to safe defaults:
the issue title, a conventional commit, and `Closes #N`.

The review gate uses a different contract. The read-only reviewer writes no
artifact. It returns one strict JSON document through `harness.Result.Output`.
An empty, malformed, or semantically invalid document is an error, never an
implicit approval. The runner supplies the diff, branch log, changed files,
lens plan, verification transcript, and convention references, then parses and
consumes the typed review outcome. Runner integration belongs to issue #33; the
review renderer and parser perform no filesystem discovery or harness calls.

## Customizing the prompt

Override the built-in template with `.romp/prompt.md` (a Go `text/template`,
configured via `[prompt] template`). Put long repo context — architecture,
conventions, invariants — in `.romp/DESIGN.md` and point `[prompt] brief` at it;
the agent is told to read it first.

When `blocked`, romp relabels the issue `romp:blocked` and posts the gap as a
comment, so the human knows exactly what to add to make the issue shippable.
