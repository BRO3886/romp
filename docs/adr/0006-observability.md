# Observability: job codenames, status, per-job logs, and gc

Status: accepted

## Context

watch runs jobs concurrently, all printing to one stderr. Once width exceeds one, the interleaved output cannot be attributed to a job, there is no way to see what is in flight, and failed jobs leak worktrees that nothing reclaims. The single-issue smoke test never surfaced this because it never ran two jobs at once. The job table already exists (ADR 0005), so status is nearly free; what is missing is a display identity per job, a place for per-job output, and a cleanup command.

## Decision

Every job gets a codename — an `adjective_name` pair such as `sunny_naruto` — derived deterministically from the repo and issue number, so the name is stable across restarts and needs no extra storage. The codename, not the issue number, is the human-facing identity: it prefixes every log line, names the per-job log file, and is the primary column in status.

status reads the job table directly (no socket in v0) and prints each in-flight job's codename, issue, branch, and elapsed time from `claimed_at`. Per-job logs live under the state dir as `logs/<codename>.log`, one file per job, so concurrent jobs no longer interleave. gc defaults to dry-run and prints what it would remove; `--apply` deletes worktrees whose job has no in-flight row. Branch deletion is out of scope for v0.

`romp run -i N` stays a pure foreground bypass: no job row, absent from status, no log file. Only watch jobs participate in observability.

## Consequences

- Concurrent jobs are attributable at a glance; status answers "what is running", and the codename ties each log line and PR back to its issue.
- The codename is deterministic, so the same issue always yields the same name and no persistence beyond the job table is needed.
- status is local-only in v0: it sees this machine's job table, not teammates' watchers. A socket (and `status --all`) is the same deferred follow-up as `cancel` and `logs` tailing, which all need a way to address a live job.
- gc reclaims worktrees only; the `romp-N` branches of failed jobs still accumulate until a later slice adds branch deletion, guarded by an open-PR check.
- The job table still stores only in-flight rows (ADR 0005), so status shows running jobs, not outcome history.
