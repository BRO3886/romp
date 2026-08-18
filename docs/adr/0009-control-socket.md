# Control socket: cancel over a Unix socket; logs tail files

Status: superseded by ADR 0010. Implemented in v0 as the interim shape: one
per-repo socket, `romp cancel` and `romp logs` commands.

## Context

ADR 0006 deferred `cancel` and `logs` behind a Unix socket: both need to address a live watcher. Before building the slice, its shape had to be decided — whether log streaming justified the socket at all, how a job is addressed, and what cancel does to the job's record and the issue's labels.

## Decision

The watcher listens on a Unix socket at `$XDG_RUNTIME_DIR/romp/<owner>-<repo>.sock`, falling back to the state dir (`~/.local/state/romp/`) when `XDG_RUNTIME_DIR` is unset, which is the normal case on macOS. `romp cancel <issue>` connects and sends the issue number; the watcher kills that job. Logs do not use the socket: `romp logs` reads the per-job log file directly, and `-f` tails the growing file — so logs works with no watcher running, and the socket serves exactly one operation.

Cancel is an abandon, not a restart: it kills the agent, records a new `cancelled` outcome, removes the claim label *and* the trigger label, deletes the job row, and removes the worktree and `romp-N` branch. The issue stays label-free until a human re-labels it, so the next poll does not re-run it. The `cancelled` outcome is deliberately distinct from `timeout`: both are romp-side kills, but timeout is romp-initiated and keeps the worktree for inspection (per the "failed jobs keep their worktree" rule), while cancel is user-initiated with full cleanup.

## Consequences

- The socket serves one verb, so the protocol is a one-shot JSON request/response with no framing or persistent connections.
- A stale socket file left by a crashed watcher is handled by connecting first and unlinking only when nothing answers, so a live watcher is never clobbered.
- `romp cancel` fails with a clear error when no watcher is running, unlike `logs`, which works file-side.
- Re-claim after cancel is gated by the label removal: the reconcile check (ADR 0003) catches an issue whose open PR survived the cancel, and the runner force-removes stale worktrees on re-add, so a cancelled issue is not re-run unless a human re-labels it.
