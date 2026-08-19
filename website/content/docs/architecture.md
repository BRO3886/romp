---
title: "Architecture"
description: "How romp works under the hood."
weight: 7
---

romp is a Go CLI that drives your local coding agent. It is deliberately
minimal: the durable source of truth is GitHub, and romp adds only the
machinery to claim, isolate, and verify work.

## Job lifecycle

<svg viewBox="0 0 860 300" role="img" aria-label="romp job lifecycle: label, poll, claim, worktree, agent, verify, then open PR, red, or blocked" xmlns="http://www.w3.org/2000/svg" style="width:100%;height:auto;max-width:860px;display:block">
  <defs>
    <marker id="a-navy" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#1a2530"/></marker>
    <marker id="a-fox" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#c0662e"/></marker>
    <marker id="a-green" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#2f7d4f"/></marker>
    <marker id="a-red" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#b3422f"/></marker>
  </defs>

  <line x1="136" y1="59" x2="163" y2="59" stroke="#1a2530" stroke-width="1.5" marker-end="url(#a-navy)"/>
  <line x1="238" y1="59" x2="265" y2="59" stroke="#1a2530" stroke-width="1.5" marker-end="url(#a-navy)"/>
  <line x1="340" y1="59" x2="367" y2="59" stroke="#1a2530" stroke-width="1.5" marker-end="url(#a-navy)"/>
  <line x1="474" y1="59" x2="501" y2="59" stroke="#1a2530" stroke-width="1.5" marker-end="url(#a-navy)"/>
  <line x1="576" y1="59" x2="603" y2="59" stroke="#1a2530" stroke-width="1.5" marker-end="url(#a-navy)"/>

  <line x1="540" y1="82" x2="505" y2="197" stroke="#c0662e" stroke-width="1.5" marker-end="url(#a-fox)"/>
  <text x="486" y="140" text-anchor="end" fill="#9a4b1c" font-family="Inter, sans-serif" font-size="12">self-reject</text>

  <line x1="642" y1="82" x2="622" y2="197" stroke="#2f7d4f" stroke-width="1.5" marker-end="url(#a-green)"/>
  <text x="610" y="140" text-anchor="end" fill="#2f7d4f" font-family="Inter, sans-serif" font-size="12">pass</text>

  <line x1="642" y1="82" x2="732" y2="197" stroke="#b3422f" stroke-width="1.5" marker-end="url(#a-red)"/>
  <text x="690" y="140" text-anchor="start" fill="#b3422f" font-family="Inter, sans-serif" font-size="12">fail</text>

  <g font-family="Inter, -apple-system, sans-serif" font-size="14" font-weight="600" fill="#1a2530" text-anchor="middle">
    <rect x="20" y="36" width="116" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="78" y="64">Label issue</text>
    <rect x="166" y="36" width="72" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="202" y="64">Poll</text>
    <rect x="268" y="36" width="72" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="304" y="64">Claim</text>
    <rect x="370" y="36" width="104" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="422" y="64">Worktree</text>
    <rect x="504" y="36" width="72" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="540" y="64">Agent</text>
    <rect x="606" y="36" width="72" height="46" rx="10" fill="#fffdf8" stroke="#1a2530" stroke-width="1.5"/>
    <text x="642" y="64">Verify</text>
  </g>

  <g font-family="Inter, -apple-system, sans-serif" font-size="14" font-weight="600" text-anchor="middle">
    <rect x="450" y="200" width="100" height="46" rx="10" fill="#f6e5cf" stroke="#c0662e" stroke-width="1.5"/>
    <text x="500" y="228" fill="#9a4b1c">blocked</text>
    <rect x="570" y="200" width="100" height="46" rx="10" fill="#e3f1e7" stroke="#2f7d4f" stroke-width="1.5"/>
    <text x="620" y="228" fill="#2f7d4f">Open PR</text>
    <rect x="700" y="200" width="70" height="46" rx="10" fill="#f5e0dc" stroke="#b3422f" stroke-width="1.5"/>
    <text x="735" y="228" fill="#b3422f">red</text>
  </g>
</svg>

1. **Poll** — `watch` lists open issues carrying the trigger label every 60
   seconds. The label is the entire queue; there is no local backlog.
2. **Claim** — atomic insert (unique on repo + issue), claim label, assign
   `@me`. Concurrent watchers on other machines skip claimed issues.
3. **Worktree** — each job runs in a fresh `git worktree` branched from the
   default branch. Never the local tree; the base is deterministic.
4. **Agent** — the goal contract is rendered and handed to the harness (Claude,
   Codex, or OpenCode).
5. **Verify** — romp re-runs every `[verify]` command itself. The agent's own
   "tests pass" is never trusted.
6. **PR or block** — green and scoped means a PR and label removal; an
   under-scoped issue becomes `blocked` with a gap comment.

## Components

```
romp/
├── cmd/romp/          # Cobra commands (init, watch, run, status, ...)
├── internal/
│   ├── config/        # TOML layering, language detection, effort validation
│   ├── harness/       # Claude, Codex, and OpenCode adapters behind a Run/Name interface
│   ├── prompt/        # goal-contract template rendering
│   ├── runner/        # job pipeline: worktree → agent → verify → PR/block
│   ├── watch/         # poll loop + claim + cancel socket
│   ├── job/           # SQLite job table + outcome history
│   ├── gh/            # GitHub client with rate-limit retry
│   ├── git/           # worktree and branch management
│   └── codename/      # deterministic adjective_name per job
└── docs/adr/          # design decisions
```

## Isolation and concurrency

- **Worktree isolation** — concurrent jobs never share a checkout.
- **Width** — an in-memory semaphore bounds concurrent jobs per repo.
- **Cross-machine dedupe** — the claim label, not any local state, is the
  authority across machines.
- **Crash recovery** — a fresh watcher clears only its own stale in-flight
  rows and reconciles issues whose `romp-N` branch already has an open PR.

## Observability

Every job gets a **codename** — an `adjective_name` pair like `sunny_naruto`,
derived deterministically from the repo and issue number. The codename prefixes
every log line, names the per-job log file, and is the primary column in
`status`.

State lives in one SQLite file per machine: `~/.local/state/romp/romp.db`. The
`jobs` table holds exactly the in-flight set; finished jobs move to the
append-only `outcomes` table in one transaction.

## Design decisions (ADRs)

| # | Decision | Status |
| --- | --- | --- |
| 0001 | An explicit verify command, independently re-run | accepted |
| 0002 | Prompt contract; outcomes via `.romp/` files | accepted |
| 0003 | GitHub is the source of truth; ephemeral worktrees | accepted |
| 0004 | TOML layered with a zero-means-default overlay | accepted |
| 0005 | Atomic claim, configurable claim label, in-flight job table | accepted |
| 0006 | Job codenames, status, per-job logs, and gc | accepted |
| 0007 | Poll the trigger label in v0; webhooks deferred | accepted |
| 0008 | Shared SQLite file and append-only outcome history | accepted |
| 0009 | Cancel over a Unix socket; logs tail files | superseded by 0010 |
| 0010 | One machine-wide daemon, one socket, HTTP clients | accepted |
