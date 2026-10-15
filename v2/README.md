# Romp v2 experiment

This is an independent TypeScript rewrite of Romp's local delivery runtime on Pi Durable 1.2.0. It lives on the remote `v2` branch. The Go implementation remains available in the repository and has no dependency on this package.

V2 runs model conversations directly through Pi. It does not invoke the Codex, Claude Code, or OpenCode CLIs. Changing the model changes the model provider, rather than selecting another installed coding agent.

## Install as `romp`

The release installer puts a `romp` launcher in `~/.local/bin` and the versioned v2 package in `~/.local/share/romp/releases`. Node.js 22.19 or newer, npm, curl, and tar must already be installed. The launcher invokes Node internally.

```sh
romp_installer="$(mktemp)"
curl -fsSL --retry 3 https://github.com/BRO3886/romp/releases/download/v2.0.0-experimental.2/install.sh -o "$romp_installer" &&
  sh "$romp_installer" &&
  rm "$romp_installer"
export PATH="$HOME/.local/bin:$PATH"
romp --help
romp cloud --help
```

Keep `~/.local/bin` on PATH in your shell configuration. Use `sh install.sh --prefix /another/location` for an alternate install directory. The installer verifies the archive checksum and version, installs the locked dependencies, and checks both CLI entry points before activation. Reinstalling switches the active package and retains previous installs. A failed download or dependency install keeps the current version active. An unrelated existing binary or application directory stops installation.

Run commands as `romp` from your target repository after installation, including `romp init`, `romp daemon start`, and `romp cloud setup`. The cloud host still requires its own account and GitHub App configuration; see [Cloud setup](cloud/README.md).

## Build and configure from source

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

## Command help and local state

The current `v2` branch uses Commander.js for commands, options, argument validation, and generated help. Use `romp --help`, `romp run --help`, or `romp cloud setup --help` to inspect the relevant command. These help commands do not open the durable runtime or create state.

Local state defaults to `~/.local/state/romp-v2`. `XDG_STATE_HOME` changes its parent directory; `ROMP_V2_STATE` overrides the complete path. On the current `v2` branch, model credentials use the OS credential vault by default. macOS uses Keychain and Linux uses Secret Service. The binding also exposes Windows Credential Manager, which has not been validated. Romp uses `@napi-rs/keyring` through native APIs; tokens are not passed as process arguments. Login prints the selected storage location. The vault service name is `com.romp.v2`.

`auth.json` now contains an opaque vault reference rather than credentials. Cloud profile and pending-session files use the same storage boundary. Existing private plaintext files migrate on first access: Romp writes the complete value to the vault, reads it back, and only then replaces the plaintext file with a reference. A failed migration preserves its source. Stop older Romp processes before upgrading; the older `.2` release cannot read the new references. Existing installations and their credentials are not migrated merely by downloading this source.

A locked or unavailable vault stops the operation. Headless environments without a credential service can explicitly select `ROMP_SECRET_STORAGE=file` before login or first setup. That fallback keeps secrets in mode-0600 files and directories in mode 0700. It does not downgrade existing vault references or export their contents. Linux requires a persistent Secret Service; Romp does not use the library's volatile kernel-keyring fallback. Keep reference files and the corresponding vault entries together; copying the files alone does not copy credentials. Environment-supplied model keys are not persisted by local model lookup.

Cloud deployment profiles and cloud login sessions use `~/.romp-v2/cloud` separately. `romp cloud login --profile personal` prepares a dedicated cloud session; it does not sign the local daemon in or create a deployed Worker. See the cloud guide for storage references and deployment steps.

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

## Cloud host

The experimental [Cloud runtime](cloud/README.md) accepts GitHub issue webhooks and runs jobs in disposable Cloudflare containers. `cloud setup --dry-run` validates account/App access and builds the deployment before provisioning. `cloud setup` deploys the Worker and connects the App webhook. `cloud doctor`, `cloud status`, `cloud configure --concurrency N`, `cloud pause`, and `cloud resume` operate the deployment using a private local profile. ChatGPT subscription support in `v2.0.0-experimental.2` uses `cloud login` followed by `cloud setup --openai-auth subscription`, with a dedicated encrypted cloud session and coordinated refresh. See the cloud guide for first-time credentials, commands, release availability, and recovery tests. The local daemon commands above remain available.
