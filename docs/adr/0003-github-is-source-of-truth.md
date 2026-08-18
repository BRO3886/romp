# GitHub is the source of truth; jobs are ephemeral worktrees

Status: accepted

## Context

romp could track pending work in a local queue and run jobs against the local working tree. Both choices create drift: a local queue falls out of sync with GitHub reality (labels removed by hand, issues closed), and the local working tree contains uncommitted state that is not what a reviewer will see.

## Decision

Pending work is "open issues carrying the trigger label": the poll returns every labelled issue, so a watcher started after a burst drains the backlog at width with no backfill mechanism. Local SQLite records only in-flight jobs. Each job runs in a fresh `git worktree` branched from origin's default branch — never the local tree — so the base is deterministic and romp only ever tests committed-and-pushed code. Before claiming, romp dedupes on issue identity (an existing job row, a `romp-N` branch, or an open PR) so a restart never spawns a duplicate agent.

## Consequences

- Backfill is free, and crash recovery is idempotent on issue identity.
- Dogfooding requires code to be pushed first; the development loop is commit → push → file issue → run → merge.
- A durable local queue is deliberately absent: the label is the only record of "todo", and label removal is the completion marker.
