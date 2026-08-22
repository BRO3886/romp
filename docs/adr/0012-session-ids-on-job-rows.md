# Session IDs recorded on job rows

Status: accepted

## Context

The runner discards the harness result entirely: `_, err = r.Harness.Run(...)` keeps only the error. When a job ends red or blocked, there is no way to answer "what did the agent actually do" without grepping raw logs, and no way to resume the agent conversation that produced the changes.

Each harness CLI exposes an identifier for its conversation, but in different shapes and output modes:

- `claude -p` reports `session_id` only under `--output-format json`; text mode omits it.
- `codex exec` prints `session id:` in its banner on stderr and emits it as `thread.started.thread_id` under `--json`.
- `opencode run --format json` carries `sessionID` on every JSONL event; text mode omits it.

## Decision

`harness.Result` gains a `SessionID string` field. Each adapter switches to its structured output mode where needed (claude to `--output-format json`, opencode to `--format json`) and parses the identifier out of the result payload. Codex parses the existing banner.

The runner records the session ID on the job row and prints it as a log line alongside the codename. The recorded ID is **truth about what ran, not a resumability guarantee**: sessions can be ephemeral (`codex exec --ephemeral`), expired, or cleaned up by the CLI, so nothing may assume a later `resume <id>` succeeds. Consumers must treat resume failure as ordinary and fall back to a fresh run.

Structured output modes change what lands in `Result.Output`, so all consumers of that field — including error paths — are updated in the same change.

## Consequences

- Red and blocked jobs become debuggable after the fact: the job row names the exact conversation.
- The fix round of the review gate (ADR 0013) can offer session resume as an experimental path (`ROMP_FIX_MODE=resume`) without new plumbing.
- rompd's protocol (ADR 0010) can expose session IDs in job payloads from day one instead of migrating a live schema later.
- Adapter parsing is coupled to each CLI's output format; format changes are an adapter concern and fail loudly at parse time, not silently.
