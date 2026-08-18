# romp

romp labels GitHub issues and opens pull requests by running a local coding agent in an isolated worktree and independently verifying the result.

## Job lifecycle

**Job**:
One labelled issue worked from claim to terminal state.
_Avoid_: task, run

**Outcome**:
The terminal state of a job: done, blocked, no-changes, red, timeout, cancelled, error.
`rate-limited` names the in-job GitHub retry, not a produced outcome.
_Avoid_: status, result

**Blocked**:
An outcome where the agent stops without writing code because the issue is ambiguous, contradictory, or under-scoped; romp relabels the issue `romp:blocked` and posts the specific gap.
_Avoid_: failed, rejected

**Cancel**:
A user-initiated action that kills a running job, records the `cancelled` outcome, removes the claim and trigger labels, and cleans up the job's worktree and branch. Cancelling abandons the issue: it stays label-free until a human re-labels it.
_Avoid_: kill, stop, abort

## Agent interface

**Harness**:
The adapter that drives one coding-agent CLI (claude, codex) behind a `Run`/`Name` interface.
_Avoid_: agent, executor, driver

**Goal contract**:
The rendered prompt handed to the harness: DONE criteria, PROVE IT commands, and CONSTRAINTS.
_Avoid_: prompt, task description

**Outcome artifact**:
A markdown file the agent writes under `.romp/` to report a structured result: `pull-request.md`, `blocked.md`.
_Avoid_: output file, result file

## Isolation and queue

**Verify command**:
The per-repo command that must pass for a job to be done, run independently by romp after the agent exits.
_Avoid_: test command, build command

**Worktree**:
The isolated git checkout, branched from the default branch, where a single job's agent runs.
_Avoid_: sandbox, checkout, workspace

**Trigger label**:
The label whose presence on an open issue marks it as pending work.
_Avoid_: romp label, work label

**Width**:
The maximum number of jobs running concurrently in a repo.
_Avoid_: parallelism, concurrency

**Admission**:
Whether the daemon may claim another job. Refused when the machine is under memory pressure.
_Avoid_: cap, slot, machine limit, free RAM

**Claim**:
Taking ownership of a labelled issue: atomically inserting a job row, then adding the claim label so no other machine works it.
_Avoid_: assign, reserve, lock

**Claim label**:
A GitHub label (`romp:claimed` by default, configurable as `claimed_label`) marking an issue as taken by a running job; the poll excludes it.
_Avoid_: in-progress label, status label

## Runtime

**Daemon**:
The single machine-wide process that watches every registered repo and owns every in-flight job.
_Avoid_: supervisor, watcher process, instance

**Registry**:
The set of GitHub origins the daemon is watching. Each origin is bound to exactly one local path.
_Avoid_: watch list, project list

**Watch**:
Enrolling a repo in the registry so the daemon polls it for work.
_Avoid_: run a watcher, start a process

**Unwatch**:
Removing an origin from the registry. In-flight jobs drain; no new claims start.
_Avoid_: stop, remove, disable

**Client**:
A process that talks to the daemon over the socket. The CLI, a menu bar app, and a desktop app are all clients. They do not read the job table themselves.
_Avoid_: frontend, UI, instance

## Observability

**Codename**:
An `adjective_name` pair (e.g. `sunny_naruto`) that identifies a job in logs and status, derived deterministically from the repo and issue number.
_Avoid_: id, name, agent-N
