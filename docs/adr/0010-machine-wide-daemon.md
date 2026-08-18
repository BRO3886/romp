# One machine-wide daemon, one socket, HTTP clients

Status: accepted

The README and ADR 0009 described one `romp watch` process per repo, each with its own Unix socket and a one-shot JSON cancel verb. That shape cannot enforce a machine-wide limit: each process has its own width semaphore, so N repos at width M become N×M harness processes and the machine OOMs. A supervisor of hidden per-repo children has the same bug unless a second protocol exists.

The daemon is the watcher. One launchd user agent polls every repo in the registry, claims work, and is the parent of every harness child (claude/codex). `romp watch` is a client verb: it enrolls the current repo and returns. The CLI, a menu bar app, and a desktop app are interchangeable HTTP clients of one Unix socket at `~/.local/state/romp/romp.sock`. Users never own a watch process. The backend does not import UI; ship clients as vertical slices against the same protocol.

The first slice includes the user agent. `romp daemon install` writes the plist (`RunAtLoad`, `KeepAlive`) and kickstarts it once. The process binds the socket itself; this is not launchd socket activation. If the socket is down, clients `launchctl kickstart` the existing agent — they do not spawn a detached `romp` and they do not rewrite the plist. `romp daemon --foreground` is the debug path and must fail if the socket is already owned.

Admission is macOS memory pressure, not a job count and not free RAM. Claim only when pressure is normal, and at most one new job per poll tick so the brake gets a vote. Warning or critical: stop claiming, do not kill running jobs. There is no TOML field and no user override. Width still bounds one repo. A registered repo with no labelled issues is a poll loop and does not consume a job slot.

A registry row is one GitHub origin bound to one absolute path. Origin is the identity (the same key as `UNIQUE(repo, issue)`). Path is a required attribute. Enrolling a second checkout of an origin already in the registry is an error. A vanished path keeps the row; the daemon skips that origin until a client rebinds it. Unwatch removes the origin from the registry immediately and stops new claims; in-flight jobs drain. Cancel is how a single job is killed.

Live control goes through the socket: watch, unwatch, status, cancel, logs. The wire is HTTP+JSON so a menu bar does not grow a custom codec; `logs -f` is a streaming response. If the daemon is down, the CLI asks launchd to start it and then sends the request. A wedged daemon makes status fail rather than return stale "running" rows.

`gc`, `doctor`, and `history` are a CLI allowlist, not a general "skip the socket" privilege. They exist to work when the daemon is dead: gc reclaims debris, doctor checks the machine, history reads the append-only outcomes table. They may open `romp.db`. They must not write claims. gc still refuses to delete a worktree that has an in-flight job row.

This supersedes ADR 0009 (per-repo socket, cancel-only, logs as files) and the README's "start one watch per repo" model. Claim semantics (ADR 0005), the shared SQLite file (ADR 0008), and `romp run -i N` as a foreground bypass (ADR 0006) are unchanged. launchd's PATH must find `gh`, `git`, and the harness; an interactive-shell-only install is a bug.

v0 shipped the ADR 0009 per-repo socket and the `cancel`/`logs` commands as an interim; the daemon replaces the socket and moves every client verb behind it.
