# Romp Cloud runtime

This experimental host accepts GitHub App `issues.opened` webhooks, queues one run per repository/issue, runs Pi Durable over an issue Durable Object's SQLite storage, and executes coding tools in a disposable Linux container. It finishes after verification and PR creation. It never merges.

The local TypeScript daemon remains available. The cloud package imports the same domain contracts, delivery task, and Pi job adapter; it does not depend on the local socket, filesystem credentials, or GitHub CLI.

## Runtime ownership

- The Worker validates the raw-body HMAC, installation, and repository allowlist before acknowledging admission.
- A scheduler Durable Object owns the installation's queue and concurrency setting. It records each reservation before dispatch. Duplicate delivery does not create another run, even after completion or cancellation.
- An issue Durable Object owns Pi storage, the frozen run input, the current workspace snapshot handle, and cleanup state. Alarms resume work and enforce the total run deadline.
- Its attached container owns the repository checkout and tool processes. The provided image contains Node 22, Git, Bash, Python, and coreutils. Repositories requiring Go, .NET, or another toolchain need an image with that toolchain.

The scheduler retains a slot until the issue host records that container destruction succeeded. Lowering concurrency lets active runs finish and stops further admission until capacity is available. A lost RPC response or expired alarm does not free a slot. Dispatch and destruction retry through persisted alarms.

## Setup and administration commands

From `v2`, install and build the CLI:

```sh
npm ci --ignore-scripts
npm run build
ROMP_CLI="$(pwd)/dist/cli.js"
node "$ROMP_CLI" cloud --help
```

First create a dedicated GitHub App, enable its webhook, subscribe to **Issues**, grant repository **Contents**, **Issues**, and **Pull requests** write permissions, and install it on the target repository. If registration requires a webhook URL, enter `https://WORKER.ACCOUNT-SUBDOMAIN.workers.dev/webhook` using the Worker name and your existing Cloudflare Workers subdomain. Keep its private key in a private file. App registration and installation are the two manual GitHub steps. Use a Cloudflare account with Workers Paid and Containers access, an existing Workers subdomain, and an API token with **Workers Scripts Edit** and **Workers Containers Write** access to that account. Docker must be running locally to build the runner image.

Set `CLOUDFLARE_API_TOKEN` and the selected provider's `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` in your environment. For a ChatGPT subscription, use the subscription flow below instead of setting `OPENAI_API_KEY`. Set `CF_ACCOUNT_ID`, `GH_APP_ID`, and `GH_APP_PRIVATE_KEY_FILE` to your real account ID, numeric App ID, and private-key path. From the target repository, create `romp-v2.toml` with explicit verification commands if it does not exist:

```sh
node "$ROMP_CLI" init --provider openai --model gpt-5.4 \
  --verify 'npm run check' --verify 'npm test'

node "$ROMP_CLI" cloud setup \
  --account "$CF_ACCOUNT_ID" --name romp-cloud \
  --app-id "$GH_APP_ID" --private-key "$GH_APP_PRIVATE_KEY_FILE" \
  --repo OWNER/REPOSITORY --dry-run
```

Choose verification commands for your repository. The supplied image supports Node and Python; use `--dockerfile /absolute/path/to/Dockerfile` for other toolchains. Repeat `--repo` to enable multiple repositories with the same configuration and image. Setup makes the cloud completion policy explicit as `review=false`, regardless of the local config's review setting. The local TOML file is not changed.

The dry run validates local configuration, the signing key and model resolver, checks account Containers access, resolves the Workers URL, verifies App permissions and event subscription, and discovers repository installation IDs. It then asks real Wrangler to build the generated configuration and image. It provisions nothing, changes no webhook, and saves no credentials. Installation-token issuance is an authentication operation; preflight makes no model calls. Read access does not prove deployment write permissions, paid capacity, or model-account availability.

Run the same setup command without `--dry-run` to deploy. Setup saves generated admin/webhook secrets before the deployment, uploads secrets with the code, checks the deployed configuration and a signed webhook ping, applies concurrency, creates missing Romp labels, and connects the App webhook last. It preserves existing labels. It uses JSON delivery with TLS verification enabled. An existing App webhook with another destination stops setup; use a dedicated App or explicitly pass `--replace-webhook` after reviewing the printed destination.

```sh
node "$ROMP_CLI" cloud setup \
  --account "$CF_ACCOUNT_ID" --name romp-cloud \
  --app-id "$GH_APP_ID" --private-key "$GH_APP_PRIVATE_KEY_FILE" \
  --repo OWNER/REPOSITORY
node "$ROMP_CLI" cloud doctor
node "$ROMP_CLI" cloud status
node "$ROMP_CLI" cloud configure --concurrency 3
node "$ROMP_CLI" cloud pause
node "$ROMP_CLI" cloud resume
```

Bare `cloud setup` works after the first deployment attempt has saved its profile. It rereads the recorded TOML and image paths, refreshes GitHub installation IDs, and repairs deployment or webhook failures using the same secrets. Profiles live outside the repository at `~/.romp-v2/cloud/default.json` with mode 0600 in a mode-0700 directory. They contain the App key, provider keys, and generated secrets. `--profile NAME` selects another independent profile. The Cloudflare API token stays in the environment. No secret values are printed or passed as command-line arguments. A lock prevents overlapping profile writes; after a killed command, remove its reported lock directory only after confirming that no command is still running.

`doctor` checks account access, App installation, webhook configuration, Worker configuration, and signed ping delivery. `status` shows queued, active, and finished reservations, errors, persisted width, and pause state. `configure` saves the selected width locally after the Worker accepts it. Pause persists across eviction, keeps new issues queued, and lets active runs finish and clean up. Resume wakes the scheduler. Redeploying preserves the pause state.

## ChatGPT subscription on Cloudflare

Subscription support requires a build from the current `v2` branch. The `v2.0.0-experimental.1` release bundle predates these commands. Use the absolute `ROMP_CLI` path from the source build above. Once a release includes this feature, its installed launcher runs the same commands as `romp cloud ...`.

Romp uses Pi's OpenAI sign-in flow on your computer and transfers a dedicated session to your personal Cloudflare deployment. This follows OpenAI's [self-hosted runtime guidance](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms). Select `provider="openai"` and a supported chat model in `romp-v2.toml`. Your plan must grant permission to use its tokens in this flow; account eligibility, available models, and subscription limits still apply.

Sign in before first setup, using the same profile for both commands:

```sh
node "$ROMP_CLI" cloud login --profile personal
node "$ROMP_CLI" cloud setup --profile personal \
  --openai-auth subscription \
  --account "$CF_ACCOUNT_ID" --name romp-cloud \
  --app-id "$GH_APP_ID" --private-key "$GH_APP_PRIVATE_KEY_FILE" \
  --repo OWNER/REPOSITORY --dry-run
```

Open the displayed OpenAI URL and finish sign-in on that computer. The callback belongs to your local browser. Run setup again without `--dry-run` to deploy and transfer the session. All the GitHub App, Cloudflare account, Docker, and verification prerequisites above still apply. Setup never transfers a session or changes the webhook during a dry run. It checks the cloud authentication after deployment and connects the webhook last.

Each profile has a stable cloud host ID under `~/.romp-v2/cloud/personal.json.auth/device-id`. This identity is separate from the local daemon's identity. Before transfer, the dedicated OAuth credential is in a mode-0600 `transfer.json` in that mode-0700 directory. Setup removes the local renewable copy after the Worker acknowledges the exact session. Preserve the profile and its host-ID directory across upgrades. Do not copy the daemon's `auth.json` to a Worker or share one rotating refresh token between runtimes.

A separate authentication Durable Object owns the cloud credential. It encrypts the credential using AES-GCM with a generated `SUBSCRIPTION_KEY` Worker secret, serializes refreshes across every issue run, and saves each rotated token before returning access. Refresh uses the issued client ID and retained scopes, as specified in OpenAI's [session documentation](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions). Only the access token reaches the model adapter. Refresh tokens never enter issue checkpoints, sandbox processes, images, Wrangler configuration, deployment secret files, or status output. Retrying a lost upload response cannot overwrite a newer rotated token. Redeployment reuses the encryption key and existing cloud session.

```sh
node "$ROMP_CLI" cloud doctor --profile personal
node "$ROMP_CLI" cloud status --profile personal
node "$ROMP_CLI" cloud configure --profile personal --concurrency 3
node "$ROMP_CLI" cloud login --profile personal
node "$ROMP_CLI" cloud logout --profile personal
```

`status` includes subscription configuration, expiry, and `requiresLogin`. `doctor` checks the configured model and resolves cloud authentication without an inference request. Access refreshes on demand; an idle deployment can require another login after its refresh token expires. Increasing concurrency shares the same subscription allowance. Cloudflare resource charges are separate from model authentication.

If refresh fails, or the actor restarts after sending a refresh whose result was not durably saved, authentication stops and requires `cloud login`. Romp does not replay a potentially consumed refresh token or fall back to an API key. A new cloud login on an existing subscription profile transfers the replacement directly. After an initial deployment or upload failure, rerun `cloud setup --profile personal`; the pending session is retained until acknowledged. If it expires before transfer, run `cloud login` again.

`cloud logout` stops further authentication, attempts remote session revocation, and clears the stored cloud and pending local credentials. If remote revocation cannot be confirmed, the command reports that result; disconnect the app in ChatGPT Settings. Requests already sent to the provider may complete. Stop active jobs first if you also want to stop their work.

API-key mode remains the default for old and new profiles. `--openai-auth api-key` selects it explicitly. Anthropic continues to use its API key. This implementation supports one active ChatGPT session per Worker deployment and is intended for a personal self-hosted installation. Live browser sign-in, account eligibility, and hosted inference require a trial with your account.

## Configuration before deployment

Install the workspace dependencies from `v2`, then run cloud checks:

```sh
npm ci --ignore-scripts
cd cloud
npm run types
npm run check
npm run lint
npm test
npm run test:container
npm run test:preflight
npm run bundle
```

Docker must be running. Tests build and remove their own images and containers. The container check uses local port 8793 and a temporary Wrangler persistence directory. `bundle` builds the deployment image and Worker without provisioning Cloudflare resources.

Configure `vars` in `wrangler.jsonc`:

- `GITHUB_APP_ID`: your numeric GitHub App ID.
- `OPENAI_AUTH`: `api-key` by default, or `subscription` for the managed session flow above.
- `CONCURRENCY`: initial installation-wide limit, from 1 to 32. A saved administrative setting overrides it.
- `REPOSITORIES`: a JSON array of `{repo, installationId, config}` objects. `config` follows `v2/src/domain/model.ts`'s `ConfigSchema`; verification commands and the model are explicit.

An empty repository list is deliberate: an unconfigured deployment cannot admit issues. The cloud completion policy requires `config.review=false`; the local runtime's post-publication review loop does not run here. Pre-publication independent review is not implemented in this first cloud host.

Set `GITHUB_WEBHOOK_SECRET`, `GITHUB_PRIVATE_KEY`, `ADMIN_TOKEN`, and the selected provider's `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` as Worker secrets. For local preflight, use an ignored `.dev.vars` file or environment variables. Only one runtime installation should own a participating repository. The webhook and administration secrets must each have at least 32 characters.

```sh
npm run preflight
```

This lower-level command supports API-key mode. Subscription mode requires `cloud setup --dry-run` or `cloud doctor` so the private profile and remote credential owner can be checked. It resolves the checked Wrangler configuration and required authentication sources without creating a container, making a model call, or provisioning cloud resources. It does not prove account access, GitHub permissions, or model quality. The setup CLI above also checks account and App access and builds the deployment image.

After preflight, deploy with Wrangler. Configure the GitHub App's webhook URL as `/webhook`, JSON delivery, and subscribe to Issues. Grant repository Contents, Issues, and Pull requests write permissions. Install it only on configured repositories. Create the configured Romp labels before running jobs.

The runtime mints short-lived GitHub installation tokens scoped to one repository. Provider keys and the app private key remain in the Worker. Git credentials are passed to individual clone/fetch/push commands, not committed into repository URLs or saved credential files. A sandbox can inspect credentials supplied to its own processes; treat its installation token as accessible during those commands.

## HTTP operations

- `GET /health`: service identity; no authentication.
- `POST /webhook`: signed GitHub App webhook admission. Unsupported events return 204; ping returns 200.
- `GET /doctor`: resolved configuration and authentication-source checks.
- `GET /status`: queue, reservations, saved limit, dispatch errors, and subscription status when enabled.
- `GET /subscription`: subscription metadata without tokens.
- `PUT /subscription`: private, bounded OAuth session transfer used by the setup CLI; requires subscription mode.
- `DELETE /subscription`: attempt remote revocation and remove the stored session.
- `PUT /concurrency`: JSON `{ "concurrency": 3 }`.
- `POST /pause` and `POST /resume`: persist or clear the scheduler's pause state.
- `GET /runs/r<repository-id>-i<issue-number>`: current job, outcome, verification events, PR URL, cancellation, and cleanup error.
- `POST /runs/r<repository-id>-i<issue-number>/cancel`: persisted cancellation.

All administrative endpoints require `Authorization: Bearer <ADMIN_TOKEN>`. Requests larger than 1 MiB are rejected before signature parsing. Concurrency requests have a 4 KiB limit; subscription imports have a 128 KiB limit. Reopening an issue or redelivering its original webhook does not retry a terminal run; explicit new-attempt support is not yet provided.

## Recovery contract

Pi commits conversations and task checkpoints into Durable Object SQLite. Workspace-changing tools and delivery operations publish a filesystem snapshot before returning their result to Pi. A restarted issue host destroys any surviving container before restoring the last published snapshot. This prevents a surviving command from racing resumed work. Interrupted unsafe tools are reported by Pi rather than replayed automatically.

Publication uses a stable branch and PR marker. If GitHub creates the PR before Pi commits its checkpoint, recovery looks up and validates that PR before proceeding. Completion first persists the job outcome, then closes Pi, destroys the sandbox, and records cleanup success. Cleanup errors remain visible and retain capacity for retry. Terminal snapshots are retained for inspection; automatic snapshot/history retention and deletion are not implemented.

Commands have a 90-second deadline and output limits of 4 MiB per stream. Cancellation destroys the sandbox to stop descendant processes. Container activity is supervised by 15-second alarms with a 120-second inactivity timeout. Native container snapshots and the Durable Object scheduling policy are beta Cloudflare features.

## Validation limits

The recovery suite runs the real Workers runtime, SQLite-backed Durable Objects, Pi Durable, Docker, shell commands, and Git. It uses Pi's scripted model provider and a persistent test GitHub port. A separate local Wrangler check exercises the production container adapter and native filesystem snapshot restoration.

Subscription tests additionally use the real OpenAI Responses adapter with HTTP contract fixtures, encrypted SQLite-backed credential storage, refresh rotation, concurrent callers, eviction, stale-upload rejection, and revocation. Their OpenAI responses are fixtures, not live account access.

Live model calls, real GitHub installation-token authorization and PR mutations, and a deployed Cloudflare failure trial are not covered. Local engine checks establish local compatibility; they do not establish hosted reliability or billing behavior.

Detailed check results and regression evidence are recorded in [VALIDATION.md](VALIDATION.md).
