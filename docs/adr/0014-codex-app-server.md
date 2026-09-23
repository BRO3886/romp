# Codex runs through app-server

Status: accepted

## Context

The Codex adapter used `codex exec --json` as a one-shot subprocess. That mode
exposed a flat event stream, but it did not give romp a protocol-level way to
verify loaded instruction files, resolve named skills, separate commentary from
the final answer, or answer an unexpected interactive request. Process success
and a plausible final JSONL sequence were the only completion evidence.

Codex app-server exposes the agent lifecycle as newline-delimited JSON-RPC over
stdio. It has explicit initialization, thread, turn, item, skill, approval, and
completion contracts. The protocol is documented at
<https://developers.openai.com/codex/app-server>.

## Decision

Each `Codex.Run` owns one `codex app-server --listen stdio://` process, one new
thread, and one turn. The adapter performs this sequence:

1. Send `initialize` and then `initialized`.
2. Resolve installed skills when the prompt contains a matching `$skill-name`,
   or when `harness.Request.Skills` requires one. Send resolved skills as
   first-class `skill` input items.
3. Start a thread in the worktree with `approvalPolicy: never`, the requested
   model, and the builder or reviewer sandbox.
4. Start a turn with the same working directory, approval policy, model,
   effort, and sandbox boundary.
5. Buffer any events that race the `turn/start` response, consume authoritative
   `item/completed` messages, and finish only on the matching
   `turn/completed` event with status `completed`.
6. Close stdin and wait for app-server. On any protocol failure, kill and reap
   the owned process before returning an empty result.

The result uses `thread.sessionId`, prefers `agentMessage` items with phase
`final_answer`, and exposes app-server's `instructionSources` plus injected
skill names through optional result metadata.

Thread and turn sandbox values deliberately use different protocol enums:
`thread/start.sandbox` is `workspace-write` or `read-only`, while
`turn/start.sandboxPolicy.type` is `workspaceWrite` or `readOnly`.

## Edge cases

| Case | One-shot exec behavior | App-server behavior |
| --- | --- | --- |
| Named skill | The model had to notice the marker and locate the skill itself. | romp force-refreshes `skills/list` and injects the resolved skill path. |
| Duplicate skill name across roots | The model's filesystem lookup decided implicitly. | romp uses app-server's first enabled match, preserving the server's discovery precedence. |
| Missing required skill | The run could continue without the requested workflow. | An explicit `Request.Skills` entry fails before thread creation. |
| Shell variables such as `$HOME` | Not applicable to the adapter. | An unmatched dollar word stays text and is not treated as a required skill. |
| Instruction files | The adapter could not report what Codex loaded. | `thread/start.instructionSources` is retained in result metadata. |
| Commentary versus final answer | The last completed agent message won. | `final_answer` items win, with the last agent message as compatibility fallback. |
| Fast completion | Event ordering was read only after process exit. | Events received before the `turn/start` response are buffered and replayed. |
| Turn failure or interruption | The adapter inferred failure from JSONL event names. | Typed `turn/completed` status and error data fail closed. |
| Approval or user-input request | A non-interactive child could stall or fail opaquely. | `approvalPolicy: never` is explicit; any unexpected server request is rejected and the run fails. |
| Malformed JSON, EOF, or wrong IDs | Parser failures happened after the child exited. | The protocol boundary fails immediately and reaps the child. |
| Cancellation | `exec.CommandContext` killed the one-shot command. | The same context owns app-server; cancellation closes the protocol and reaps the server. |
| Successful turn followed by bad server exit | The command exit was the only success boundary. | A completed turn still fails if owned process shutdown fails. |

## Consequences

- The harness interface stays vendor-neutral while the Codex adapter owns the
  vendor protocol.
- Builder and reviewer isolation is explicit at both thread and turn creation.
- Installed skill and instruction behavior is observable and testable.
- The adapter depends on the experimental app-server command and must track its
  protocol compatibility. `Check` verifies that the installed CLI exposes the
  command before a job starts.
- A process is still scoped to one Romp run. This decision does not introduce a
  shared Codex daemon, session resume, steering UI, or approval UI.
