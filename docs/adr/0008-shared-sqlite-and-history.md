# Shared SQLite file and append-only outcome history

Status: accepted

## Context

ADR 0005 placed one SQLite file per repo at `~/.local/state/romp/<owner>-<name>/jobs.db`, and the repo column inside each file was redundant with the directory. That layout served the in-flight-only data model, but two things changed. First, `status --all` had to glob `*/jobs.db` and open each file to answer a question that is naturally one query. Second, romp has no record of what it did: a finished job's row is deleted, so "which PR did issue 5 get, and did it pass" is unanswerable. History is inherently cross-repo — "all PRs yesterday" spans files — and the per-repo layout makes that a merge, not a query. The question was whether to keep per-repo files or consolidate.

## Decision

One SQLite file per machine at `~/.local/state/romp/romp.db`, shared by every repo and distinguished by the repo column. `job.Path()` now takes no arguments and returns that single path; `job.DBs()` (the glob) is gone.

The claim semantics of ADR 0005 are unchanged and deliberately kept in a separate table. History cannot live in the jobs table: its `UNIQUE(repo, issue)` constraint is the same-machine claim serialization point, and keeping terminal rows would make a re-claimed issue violate it. So the jobs table stays exactly the in-flight set, and a new append-only `outcomes` table records finished jobs. `Finish` moves a job from in-flight to history in one transaction, carrying the row's claim timestamp over as `started_at`, and records outcome, branch, pr_url, detail, and finished_at. Outcomes follow the README taxonomy — done, blocked, no-changes, red, timeout, cancelled — with uncategorized failures (git or gh infrastructure errors) recorded as "error". The rate-limited outcome is not yet produced as a distinct classification.

`ClearRunning` became repo-scoped (`ClearRunning(repo)`): in the shared file, an unscoped clear on one watcher's startup would wipe another repo's in-flight rows. A `romp history` command lists the most recent finished jobs, and `status --all` is now a single filtered query instead of a glob.

## Consequences

- Cross-repo queries are one query; `status --all` and `history` read the same file, and there is a single schema and migration path.
- Write contention between repos returns (SQLite is single-writer per file). At personal-tool scale with WAL and `busy_timeout`, this is negligible; the correctness cost of per-repo files — the glob, the redundant repo column, the N-way migration — outweighed it once history existed.
- The per-repo `jobs.db` files from ADR 0005 are orphaned on machines that already ran romp. They hold only disposable in-flight rows (cleared on every watcher start), so they are left in place rather than migrated; logs stay in the per-repo `logs/` directories.
- History is bounded by gc, not unbounded: `romp gc` prunes outcomes older than 30 days by default (dry-run unless `--apply`), following the same dry-run/apply shape as worktree cleanup. The window is a machine-wide setting, `history_days`, read from the user config (`~/.config/romp/config.toml`) and overridable per run with `--history-days`; it is deliberately not settable in the per-repo `romp.toml`, because retention is an operator concern, not a team convention. The table records only what the taxonomy names, so a future report ("what got done last week") has the raw material for whatever the retention window keeps.
- `romp run -i N` (the foreground bypass) still records no history, consistent with ADR 0006's boundary: only watch jobs participate in observability.