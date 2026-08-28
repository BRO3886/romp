# Session IDs recorded on job rows

Status: accepted

## Context

The runner discards the harness result entirely: `_, err = r.Harness.Run(...)` keeps only the error. When a job ends red or blocked, there is no durable answer to "what did the agent actually do" and no conversation handle for debugging or later experiments.

Each harness CLI exposes an identifier for its conversation, but in different shapes and output modes:

- `claude -p` reports `session_id` only under `--output-format json`; text mode omits it.
- `codex exec --json` emits JSONL on stdout. Its `thread.started` event carries the identifier in `thread_id`.
- `opencode run --format json` carries `sessionID` on every JSONL event; text mode omits it.

## Decision

`harness.Result` gains a `SessionID string` field. Each adapter uses the CLI's structured transport and normalizes it at the adapter boundary:

- Claude uses `--output-format json`, reads `session_id`, and returns `result` as the final assistant text.
- Codex uses `exec --json`, reads `thread.started.thread_id` from the JSONL event stream, and returns the last completed `agent_message` as the final assistant text. `--output-schema` is a separate option that constrains the final agent message; it does not replace the JSONL transport.
- OpenCode uses `run --format json`, reads `sessionID` from the JSONL events, and returns the ordered text parts as the final assistant text.

All adapters keep stdout and stderr in separate buffers. Structured parsing reads stdout only, while command and parse failures retain raw stdout and stderr as labelled diagnostics.

After a successful harness run, the runner records the session ID on the active `jobs` row and prints it as a codename-prefixed log line. A failed CLI run does neither, even if its partial output contains an identifier. `Store.Finish` copies the active row's session ID into the new `outcomes` row in the same transaction before it deletes the active row. Both columns are nullable, and opening a database created by an earlier release adds either missing column without rebuilding or replacing the tables. Active status and finished history display the identifier when it exists.

The recorded ID is **truth about what ran, not a resumability guarantee**. A session can be ephemeral, expired, cleaned up, or rejected by a later CLI version. No code path assumes that `resume <id>` succeeds, and this decision does not implement resume.

## Consequences

- Red and blocked outcomes retain the exact conversation identifier after their active rows are deleted.
- A successful run exposes its identifier while later job steps, such as verification and PR creation, are still running.
- Existing shared databases migrate in place and preserve their active jobs and outcome history.
- Adapter parsing is coupled to each CLI's output format; format changes are an adapter concern and fail loudly at parse time, not silently.
- Exposing session IDs through the rompd protocol remains separate ADR 0010 implementation work.
