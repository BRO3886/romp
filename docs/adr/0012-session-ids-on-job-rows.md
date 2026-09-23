# Session IDs recorded on job rows

Status: accepted

## Context

The runner discards the harness result entirely: `_, err = r.Harness.Run(...)` keeps only the error. When a job ends red or blocked, there is no durable answer to "what did the agent actually do" and no conversation handle for debugging or later experiments.

Each harness CLI exposes an identifier for its conversation, but in different shapes and output modes:

- `claude -p` reports `session_id` only under `--output-format json`; text mode omits it.
- Codex app-server returns `thread.sessionId` from `thread/start`.
- `opencode run --format json` carries `sessionID` on every JSONL event; text mode omits it.

## Decision

`harness.Result` gains a `SessionID string` field. Each adapter uses the CLI's structured transport and normalizes it at the adapter boundary:

- Claude uses `--output-format json`, reads `session_id`, and returns `result` as the final assistant text.
- Codex uses app-server, reads `thread.sessionId`, and returns completed
  `agentMessage` items from a successfully completed turn. See ADR 0014 for
  the protocol and lifecycle decision.
- OpenCode uses `run --format json`, reads `sessionID` from the JSONL events, and returns the ordered text parts as the final assistant text.

All adapters keep protocol output and stderr separate. Command and protocol
failures retain stderr as a labelled diagnostic.

After a successful harness run, the runner records the session ID on the active `jobs` row and prints it as a codename-prefixed log line. A failed CLI run does neither, even if its partial output contains an identifier. `Store.Finish` copies the active row's session ID into the new `outcomes` row in the same transaction before it deletes the active row. Both columns are nullable, and opening a database created by an earlier release adds either missing column without rebuilding or replacing the tables. Active status and finished history display the identifier when it exists.

The recorded ID is **truth about what ran, not a resumability guarantee**. A session can be ephemeral, expired, cleaned up, or rejected by a later CLI version. No code path assumes that `resume <id>` succeeds, and this decision does not implement resume.

## Consequences

- Red and blocked outcomes retain the exact conversation identifier after their active rows are deleted.
- A successful run exposes its identifier while later job steps, such as verification and PR creation, are still running.
- Existing shared databases migrate in place and preserve their active jobs and outcome history.
- Adapter parsing is coupled to each CLI's output format; format changes are an adapter concern and fail loudly at parse time, not silently.
- Exposing session IDs through the rompd protocol remains separate ADR 0010 implementation work.
