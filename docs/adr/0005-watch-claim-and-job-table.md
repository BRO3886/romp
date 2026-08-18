# The watch loop: atomic claim, a configurable claim label, and an in-flight job table

Status: accepted

## Context

watch is romp's core loop: poll for labelled issues, run each in a worktree at width, open a PR. The same issue can be worked twice — by two goroutines in one watcher, by a restarted watcher, or by two teammates' watchers on different machines. Claiming must be atomic, dedupe must cross process and machine boundaries, and the job table must be exactly the in-flight set.

## Decision

Claim is two coordinated writes: an INSERT of a job row carrying a UNIQUE(repo, issue) constraint — a losing INSERT skips, serializing same-machine watchers — followed by adding a claim label to the issue, the cross-machine signal since two teammates do not share a state dir. The claim label defaults to `romp:claimed`, is configurable as `claimed_label`, and is created by `romp init` alongside the trigger label. The poll predicate is "has the trigger label and does not have the claim label". The job table holds only in-flight jobs (insert on claim, delete on terminal), stored in SQLite via modernc.org/sqlite with WAL, synchronous=NORMAL, and foreign_keys=ON. Width is an in-memory semaphore channel, not the table. `romp run -i N` is a pure foreground bypass: no job row, no dedupe; it only cleans its own stale romp-N branch and worktree.

Shutdown drains: the first signal stops claiming and waits for running jobs — each job gets a context detached from the signal so cancellation does not kill the agent — and the second signal cancels all jobs.

## Consequences

- Claim is transactional on one machine (UNIQUE row) and visible on GitHub (the claim label), so concurrent watchers on different machines are supported: each skips issues the others have claimed.
- A fresh watcher never un-claims; it clears only its own stale in-flight rows, which live in the local state dir and are meaningless to other machines. The claim label is therefore authoritative across machines.
- A crashed watcher orphans its claim label: the issue stays marked claimed until a human unlabels it, because there is no lease to distinguish a dead claim from a live one. The orphan is visible on the issue, not silent. Resume — re-dispatching a half-finished job with a continuation prompt — and a lease/timestamp to auto-expire stale claims are the same deferred follow-up, recorded here as a deliberate v0 omission.
- The job table is the substrate for status/cancel/logs later; terminal rows are deleted for now, so there is no outcome history, only in-flight state.
