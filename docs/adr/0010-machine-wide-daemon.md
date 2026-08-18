# One machine-wide daemon, one socket, HTTP clients

Status: accepted

## Context

The README and ADR 0009 described one `romp watch` process per repo, each with its own Unix socket and a one-shot JSON cancel verb. That shape cannot enforce a machine-wide limit: each process has its own width semaphore, so N repos at width M become N×M harness processes and the machine OOMs. A supervisor of hidden per-repo children has the same bug. The end state is a login-session runtime that a CLI, a menu bar, and a desktop app can all talk to, without the user owning a watch process.

launchd does not load an interactive shell, so it does not see Homebrew or `~/.local/bin`. romp finds `git`, `gh`, and the harness with `exec.LookPath` on `PATH`. A doctor run in the terminal will pass on a machine where the daemon then fails with `claude CLI not found`.

## Decision

The daemon is the watcher. One launchd user agent polls every repo in the registry, claims work, and is the parent of every harness child. `romp watch` enrolls the current repo and returns. The CLI, a menu bar, and a desktop app are HTTP clients of one Unix socket at `~/.local/state/romp/romp.sock`. The backend does not import UI.

`romp daemon install` writes the user-agent plist (`RunAtLoad`, `KeepAlive`) and kickstarts it. The process binds the socket itself. Clients never spawn a detached daemon and never rewrite the plist; if the socket is down they `launchctl kickstart` the existing agent, and if the agent is not installed they error. `romp daemon --foreground` fails if the socket is already owned.

Install resolves `romp`, `git`, `gh`, and the harness with the installing shell's `LookPath`. The plist `Program` is the absolute path to `romp`. `EnvironmentVariables.PATH` is the unique parent directories of those binaries, plus `/usr/bin:/bin` — not a dump of the interactive `PATH`. Re-run install after a binary moves. `romp doctor` must ask the running daemon what *it* can resolve; a CLI-only doctor is not sufficient.

Admission is macOS memory pressure, not a job count and not free RAM. Claim only when pressure is normal, and at most one new job per poll tick. Warning or critical: stop claiming, do not kill running jobs. No TOML field, no user override. Width still bounds one repo.

A registry row is one GitHub origin bound to one absolute path. Origin is the identity. Path is required. A second checkout of an origin already in the registry is an error. A vanished path keeps the row; the daemon skips that origin until a client rebinds it. Unwatch removes the origin and stops new claims; in-flight jobs drain.

Live control goes through the socket: watch, unwatch, status, cancel, logs. The wire is HTTP+JSON. `gc`, `doctor`, and `history` are a closed CLI allowlist and may open `romp.db`; they must not write claims. `romp run -i N` stays a foreground bypass (ADR 0006).

This supersedes ADR 0009. Claim semantics (ADR 0005) and the shared SQLite file (ADR 0008) are unchanged.

## Consequences

- One panic stops every repo. Acceptable because the poll loop is thin, the memory hog is the harness child, and launchd restarts the agent.
- First-run has one extra command (`romp daemon install`). That is the real service, not a rehearsal for a later plist.
- A quiet 64GB machine ramps one job per poll tick. That is the cost of no job-count knob.
- Two readers of `romp.db` exist (the daemon, and the gc/doctor/history allowlist). Adding a command to that list is a decision. The menu bar never opens the db.
- A harness installed only inside nvm, or moved after install, is invisible until install is re-run. Dumping the full interactive PATH was rejected because those entries rot.
- The ADR 0009 per-repo cancel socket is an interim on main; the daemon replaces it, it does not sit beside it.
