# Daemon protocol v1

Clients connect to `~/.local/state/romp/romp.sock`, or the same path below
`$XDG_STATE_HOME`. Each UTF-8 JSON-RPC 2.0 message occupies one line. Frames are
bounded at 8 MiB. The state directory is mode 0700 and the socket is mode 0600.
The macOS server verifies the peer's effective user ID before accepting requests.

The first request is `initialize`, with client name, client version, an inclusive
protocol range, and capabilities. The daemon selects v1 or returns error -32001.
Requests before initialization return -32002. Unknown methods return -32601 and
invalid parameters return -32602. Clients receive no offline storage fallback.

`v1.schema.json` defines the records and method catalogue. Run
`make generate-protocol` to regenerate `internal/protocol/types_generated.go` and
`Types.generated.swift`. Go persistence types alias the generated wire records.
The Swift records use explicit CodingKeys; decoder key-conversion strategies are
unnecessary. Review durations are integer nanoseconds in review summaries and
integer milliseconds in per-pass instrumentation. Timestamps are RFC 3339 strings.

| Methods | Result |
| --- | --- |
| `daemon.health`, `daemon.version`, `daemon.restartReady` | Version, health, pressure, active count, and idle readiness |
| `daemon.prepareRestart` | Stop admission and wait for active work to finish without cancellation |
| `daemon.prepareShutdown` | Cancel jobs, wait for recorded outcomes and cleanup, then acknowledge readiness |
| `registry.list` | Persistent origin/path rows with runtime health |
| `registry.watch`, `registry.rebind`, `registry.unwatch` | Register, move, or stop admission for an origin |
| `jobs.list`, `jobs.history`, `jobs.reviewSummary` | Active rows, recent outcomes, or calibration |
| `jobs.run` | Start an explicit issue and return its origin, issue, and log name |
| `jobs.cancel` | Request abandonment of an active issue |
| `logs.snapshot` | Up to 1 MiB of log text, next byte offset, and EOF flag |
| `logs.subscribe` | The same snapshot; subscription begins atomically when EOF is reached |
| `doctor` | Daemon-side tool, auth, configuration, and environment checks |
| `gc` | Planned cleanup, or applied cleanup when `apply` is true |
| `subscribe` | Enable job, registry, memory, and shutdown notifications |

Pass absolute checkout paths for watch, rebind, run, doctor, and GC. Origin is
`owner/name`. A second checkout of a watched origin requires an explicit idle
rebind. `repo` filters job queries; omission means all repositories. History
defaults to 20 entries and is bounded at 1000. Review-summary `days` must be
positive. GC uses `days = -1` for user-config retention and `days = 0` to disable
pruning. Branch deletion remains outside GC.

Log requests identify `repo` and a codename in `name`; offsets are bytes. Repeat
snapshot requests until EOF. A subscribing client must buffer notifications that
arrive before the snapshot response, write the snapshot first, then flush those
records. Notifications have no request ID. Clients disconnect if the bounded
notification queue overflows, and can reconnect and fetch snapshots. Disconnecting
does not cancel server work.

General notifications are `jobs.changed`, `jobs.finished`, `registry.changed`,
`registry.health`, `memory.changed`, and `daemon.shutdown`. `logs.record` goes
only to the connection that subscribed to that repo and log name. An explicit
run ends with `jobs.finished`; the client must preserve the terminal outcome
instead of inferring success from EOF.

`make smoke` checks built Go and Swift clients against the current daemon.
`python3 scripts/smoke_daemon.py --launchd` additionally checks the real macOS
launchd engine, KeepAlive restart after SIGKILL, and explicit bootout. Both use
temporary state and remove their process and agent on completion. The initial
v1 fixtures are in `internal/daemon/server_test.go` and
`scripts/protocol_smoke.swift`; no older daemon protocol was released.
