# Romp v2 experiment

This is an independent TypeScript rewrite of Romp's local delivery runtime on Pi Durable 1.2.0. It lives on the remote `v2` branch. The Go implementation remains available in the repository and has no dependency on this package.

V2 runs model conversations directly through Pi. It does not invoke the Codex, Claude Code, or OpenCode CLIs. Changing the model changes the model provider, rather than selecting another installed coding agent.

## Build and configure

Requires macOS or Linux, Node 22.19 or newer, git, and an authenticated GitHub CLI with a GitHub.com origin remote. Windows and GitHub Enterprise are not supported by this experiment.

```sh
cd v2
npm ci --ignore-scripts
npm run build
node dist/cli.js --help
node dist/cli.js models --provider openai
```

Use an absolute path to the compiled CLI when operating on another repository. Run `init` from that repository, supplying its actual verification commands and a model returned by `models`. For example, in a Go repository:

```sh
node /path/to/romp/v2/dist/cli.js init \
  --provider openai --model gpt-6.1-sol \
  --verify "go build ./..." --verify "go test ./..."
```

The `/path/to/romp` prefix denotes your checkout location. `init` creates `romp-v2.toml` and refuses to overwrite an existing file. It does not create labels, change gitignore, or guess verification commands. Commit this configuration to the target repository so the worktree contains the same contract.

## Authentication and preflight

The registered providers are OpenAI and Anthropic. API keys supplied through their normal environment variables work. For Pi's provider-owned OAuth flows:

```sh
node dist/cli.js login --provider openai
node dist/cli.js logout --provider openai
```

Stop the daemon before changing login state. V2 stores its own credentials in its private state directory; it does not read or alter another coding agent's login. Provider access, model availability, and billing depend on the selected authentication source. The presence of an OAuth flow does not establish access for a particular account.

From the target repository, run `doctor` with the absolute CLI path before starting work. It resolves the real repository, configuration, GitHub identity, model IDs, and authentication without making a model request or creating a job, worktree, label, or PR. Refreshing an existing OAuth credential can update that credential.

## Run and watch

Start the daemon in a persistent terminal with the same environment that holds any API keys:

```sh
node dist/cli.js daemon start --width 1
```

From the target repository, use the absolute CLI path to run `run --issue 17`. Add `--detach` to return the job ID immediately. `wait ID` reconnects to completion. `status`, `history`, `job ID`, and `logs ID` inspect persisted job state and reports. `cancel ID` aborts the job and its agent/tool ownership tree. `steer ID MESSAGE` submits a message to an active builder.

Create the four configured labels before running jobs: by default `romp:v2`, `romp:v2:claimed`, `romp:v2:blocked`, and `romp:v2:changes-requested`. `watch` registers the current origin and returns. The daemon polls registered repositories once per minute. `unwatch` stops future admission and lets active jobs continue. `daemon stop`, SIGINT, and SIGTERM suspend unfinished tasks; the next start resumes them. Closing a client never cancels its job.

State defaults to `$XDG_STATE_HOME/romp-v2` or `~/.local/state/romp-v2`. `ROMP_V2_STATE` selects a different directory. One daemon owns that directory, its SQLite storage, and its Unix socket. The directory and socket have permissions 0700 and 0600 respectively. V1 state, sockets, and labels are separate.

## Delivery and recovery

Each job snapshots its configuration, issue, base commit, branch, and worktree identity. The durable delivery task progresses through preparation, building, artifact validation, committing, verification, publication, review, repair, and completion. Agent conversations belong to that task and use stable request IDs across restarts.

The builder writes `.romp/pull-request.md` or `.romp/blocked.md`. PR metadata must satisfy the artifact contract; missing or malformed metadata fails the job. Romp consumes the artifact before committing and independently runs the configured verification commands. It records each result and the verified commit. A changed HEAD or dirty worktree prevents publication.

After verification, Romp pushes the branch and opens the PR. It then runs an independent reviewer with only the read tool and an environment that rejects shell execution and mutation methods. The reviewer receives the issue, diff, verification transcript, and builder report. Blocking findings trigger bounded repair rounds, each followed by independent verification and another review. Parsed review passes become separate PR comments. Romp never merges.

On recovery, publication looks up the existing PR by its unique job branch and checks its base, state, and job marker before reusing it. Comments carry distinct job/pass markers and are reconciled before posting. These checks recover the interval between a successful GitHub write and a missing local checkpoint. They do not make arbitrary external effects exactly once.

Shell commands run under a supervisor in an owned process group. The supervisor writes its receipt before starting the command and stops the group if the daemon dies. Startup checks recorded groups and stops survivors before resuming tasks. A process that deliberately daemonizes outside that group is outside this guarantee.

Successful, blocked, and no-change jobs remove their worktree. Failed, red, rejected, and cancelled jobs keep it for inspection. Branches are retained. A failed terminal cleanup remains visible as a failed job; inspect its detail and GitHub claim before retrying.

## Verification and limits

```sh
npm run check
npm run lint
npm run format:check
npm test
npm audit --omit=dev
```

Tests use the real Pi Durable runtime, Node SQLite, coding tools, git repositories and remotes, shell processes, and Unix sockets. The model provider is Pi's scripted test provider. GitHub mutations use a test implementation of the application port. Recovery tests send SIGKILL during a shell tool and after PR creation. They verify process cleanup, continuation, and publication reconciliation.

This experiment does not claim V1 feature parity. It has one reviewer per pass rather than V1's lens fan-out. Launchd packaging, the native app protocol, the TUI, skill installation, configuration layering, protected/ignored path configuration, memory-pressure admission, and GC are not implemented here. History is currently retained without a size limit. Global width defaults to one.

GitHub labels are a visible claim signal, not an atomic cross-machine lock. Use one v2 daemon per participating repository. Live model quality, account-specific OAuth access, and hosted GitHub mutation behavior require a separate end-to-end trial. The automated suite does not prove those properties.

The module boundaries, validated contracts, and persistence migration are documented in [ARCHITECTURE.md](ARCHITECTURE.md). Source and tests are typechecked. CI checks types, lint, formatting, and behavior on the remote `v2` branch.
