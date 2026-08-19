# One open-source machine-wide daemon, one protocol, multiple clients

Status: accepted

## Context

The README and ADR 0009 described one `romp watch` process per repo, each with its own Unix socket and a one-shot JSON cancel verb. That shape cannot enforce a machine-wide limit: each process has its own width semaphore, so N repos at width M become N×M harness processes and the machine can run out of memory. A supervisor of hidden per-repo children has the same defect.

romp also has two intended interfaces. Developers must be able to install and use an open-source CLI without buying another product. People who want automatic setup, notifications, and a native menu-bar workflow can buy a directly distributed macOS app. These interfaces must not grow separate implementations of persistence, scheduling, logs, recovery, or errors.

The resulting boundary is an open-source local server consumed by multiple clients. The CLI and the paid app differ in convenience and presentation, not in backend capability.

launchd does not load an interactive shell, so it does not inherit Homebrew, version-manager, or user-local paths. A doctor run in a terminal can pass on a machine where the daemon later fails with `claude CLI not found`. Installation and runtime health must therefore describe the daemon's environment, not the client's environment.

## Decision

### Server and process ownership

`rompd` is the open-source backend server. One machine-wide `rompd` process polls every repo in the registry, claims work, owns every harness child, and is the only process that opens `romp.db` or reads and writes job logs. It contains no UI code.

The CLI and menu-bar app are protocol clients. `watch`, `unwatch`, `rebind`, `status`, `history`, `cancel`, `logs`, `doctor`, `gc`, and foreground `run` behavior go through `rompd`; clients do not contain offline database or file fallbacks. `romp run -i N` remains outside registry polling and label-triggered admission, as decided in ADR 0006, but the server owns its execution.

`rompd` runs as a launchd user agent with `RunAtLoad` and `KeepAlive`. Quitting the menu-bar app disconnects that client and does not stop the daemon or its jobs. `romp daemon stop` and the app's explicit **Stop romp** action use one two-phase lifecycle contract: request cancellation through the server, wait until every active job has recorded its `cancelled` outcome and completed the cleanup from ADR 0009, then boot the launchd agent out of the current login session. Exiting `rompd` without booting out the agent is not a stop because `KeepAlive` restarts it. Uninstall is a separate action that removes the plist and canonical daemon binary.

An abnormal exit cannot perform cancellation cleanup. On restart, `rompd` moves stale in-flight rows to a new `interrupted` outcome and preserves their worktrees, branches, and trigger labels for inspection or retry. A crash, forced kill, power loss, or machine restart is never reported as a user cancellation.

### Distribution and installation

The open-source distribution contains the `romp` CLI and `rompd`. CLI users install it independently and manage the daemon with `romp daemon install`, `start`, `stop`, `restart`, `status`, and `version`.

The directly distributed, notarized macOS app bundles the same open-source `romp` and `rompd` artifacts. First launch installs and starts the daemon without requiring a terminal command. The app also makes its bundled CLI available to app users without overwriting an existing `romp` command. The app may remain proprietary and charge for installation, lifecycle management, notifications, updates, and native UI; the backend and CLI remain usable without it.

Both distributions atomically install one canonical daemon binary at `~/Library/Application Support/romp/bin/rompd`. The launchd plist always points to this stable path, never into an app bundle, Homebrew prefix, Go bin directory, or other client-owned location. Installers refuse accidental downgrades. A newer compatible binary can be staged while jobs run, but daemon restart waits until no jobs are active. An incompatible client reports the version mismatch and requires that client to be updated; it never replaces the daemon with an older build.

Only the open-source daemon installer writes or updates the launchd plist. The app invokes that installer contract instead of implementing a second plist writer.

Install resolves `git`, `gh`, configured harnesses, and the execution environment from the initiating user's login environment, then persists the absolute program paths and effective `PATH` used by the daemon. The plist `Program` is the canonical absolute path to `rompd`. Re-running installation refreshes paths after tools move. `doctor` asks the running daemon what it can resolve and execute; a client-only doctor is insufficient.

### Protocol

Clients communicate with `rompd` through a versioned JSON-RPC 2.0 protocol over one Unix socket at `~/.local/state/romp/romp.sock`. The state directory is mode `0700`, the socket is mode `0600`, and the server rejects peers whose effective user ID differs from its own.

Every connection begins with `initialize`. The request includes the client name, client version, supported protocol range, and capabilities. The response includes the daemon version, selected protocol version, server capabilities, and runtime health. No other request is accepted before initialization.

The protocol covers:

- daemon health, version, shutdown preparation, and restart readiness;
- registry list, watch, unwatch, and path rebind;
- active jobs and recent outcomes;
- foreground run and cancellation;
- log snapshots and subscriptions;
- doctor checks; and
- gc planning and application.

Server notifications publish job changes, log records, registry health, memory-pressure state, and daemon shutdown. JSON Schema in the open-source repository is the protocol source of truth and generates Go and Swift client types. A protocol compatibility test runs every released CLI and app client fixture against the supported daemon versions.

### Registry and scheduling

A registry row is one GitHub origin bound to one absolute path. Origin is the identity. Path is required. A second checkout of an origin already in the registry is an error. A vanished path keeps the row; the daemon reports it unhealthy and skips that origin until a client rebinds it. Unwatch removes the origin and stops new claims; in-flight jobs drain.

Admission uses macOS memory pressure, not a job count or free-RAM estimate. The daemon claims only while pressure is normal and admits at most one new job per global poll tick. Warning or critical pressure stops new claims without killing running jobs. There is no TOML field or user override. Width still bounds concurrent jobs within one repo. The daemon rotates the first repo considered on each tick so a busy repo cannot starve later registry entries.

This supersedes ADR 0009. Claim semantics from ADR 0005 and the shared SQLite file from ADR 0008 remain, except that only `rompd` may access the database and `interrupted` joins the outcome taxonomy.

## Consequences

- The CLI and paid app consume one backend contract. A new interface does not get a second storage or scheduling implementation.
- App users install one app and receive both the GUI and CLI. CLI-only users retain the complete open-source backend and automation surface.
- One daemon panic affects every repo. launchd restarts the server, and restart reconciliation records truthful `interrupted` outcomes instead of silently clearing jobs.
- The menu-bar app can quit without terminating work. Stopping all work is a separate, deliberate action.
- Two distributions can carry `rompd`, but neither owns the live installation path. Atomic installation, downgrade protection, capability negotiation, and idle restart prevent last-installer-wins behavior.
- JSON-RPC notifications give the app live state and logs without a separate polling or SSE contract. The cost is maintaining protocol framing, schemas, generated clients, and compatibility fixtures.
- A harness or tool installed through a version manager can move after installation. The daemon reports the stale absolute path until installation refreshes its environment.
- The ADR 0009 per-repo cancel socket is an interim implementation on main. The machine-wide protocol replaces it rather than running beside it.
