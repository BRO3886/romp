# Romp Cloud runtime

This experimental host accepts GitHub App `issues.opened` webhooks, queues one run per repository/issue, runs Pi Durable over an issue Durable Object's SQLite storage, and executes coding tools in a disposable Linux container. It finishes after verification and PR creation. It never merges.

The local TypeScript daemon remains available. The cloud package imports the same domain contracts, delivery task, and Pi job adapter; it does not depend on the local socket, filesystem credentials, or GitHub CLI.

## Runtime ownership

- The Worker validates the raw-body HMAC, installation, and repository allowlist before acknowledging admission.
- A scheduler Durable Object owns the installation's queue and concurrency setting. It records each reservation before dispatch. Duplicate delivery does not create another run, even after completion or cancellation.
- An issue Durable Object owns Pi storage, the frozen run input, the current workspace snapshot handle, and cleanup state. Alarms resume work and enforce the total run deadline.
- Its attached container owns the repository checkout and tool processes. The provided image contains Node 22, Git, Bash, Python, and coreutils. Repositories requiring Go, .NET, or another toolchain need an image with that toolchain.

The scheduler retains a slot until the issue host records that container destruction succeeded. Lowering concurrency lets active runs finish and stops further admission until capacity is available. A lost RPC response or expired alarm does not free a slot. Dispatch and destruction retry through persisted alarms.

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
- `CONCURRENCY`: initial installation-wide limit, from 1 to 32. A saved administrative setting overrides it.
- `REPOSITORIES`: a JSON array of `{repo, installationId, config}` objects. `config` follows `v2/src/domain/model.ts`'s `ConfigSchema`; verification commands and the model are explicit.

An empty repository list is deliberate: an unconfigured deployment cannot admit issues. The cloud completion policy requires `config.review=false`; the local runtime's post-publication review loop does not run here. Pre-publication independent review is not implemented in this first cloud host.

Set `GITHUB_WEBHOOK_SECRET`, `GITHUB_PRIVATE_KEY`, `ADMIN_TOKEN`, and the selected provider's `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` as Worker secrets. For local preflight, use an ignored `.dev.vars` file or environment variables. Only one runtime installation should own a participating repository. The webhook and administration secrets must each have at least 32 characters.

```sh
npm run preflight
```

This resolves the real configuration and required authentication sources without creating a container, making a model call, or provisioning cloud resources. It does not prove account access, GitHub permissions, or model quality. The guided setup command and GitHub App creation are separate future work.

After preflight, deploy with Wrangler. Configure the GitHub App's webhook URL as `/webhook`, JSON delivery, and subscribe to Issues. Grant repository Contents, Issues, and Pull requests write permissions. Install it only on configured repositories. Create the configured Romp labels before running jobs.

The runtime mints short-lived GitHub installation tokens scoped to one repository. Provider keys and the app private key remain in the Worker. Git credentials are passed to individual clone/fetch/push commands, not committed into repository URLs or saved credential files. A sandbox can inspect credentials supplied to its own processes; treat its installation token as accessible during those commands.

## HTTP operations

- `GET /health`: service identity; no authentication.
- `POST /webhook`: signed GitHub App webhook admission. Unsupported events return 204; ping returns 200.
- `GET /doctor`: resolved configuration and authentication-source checks.
- `GET /status`: queue, reservations, saved limit, and dispatch errors.
- `PUT /concurrency`: JSON `{ "concurrency": 3 }`.
- `GET /runs/r<repository-id>-i<issue-number>`: current job, outcome, verification events, PR URL, cancellation, and cleanup error.
- `POST /runs/r<repository-id>-i<issue-number>/cancel`: persisted cancellation.

All administrative endpoints require `Authorization: Bearer <ADMIN_TOKEN>`. Requests larger than 1 MiB are rejected before signature parsing. Concurrency requests have a 4 KiB limit. Reopening an issue or redelivering its original webhook does not retry a terminal run; explicit new-attempt support is not yet provided.

## Recovery contract

Pi commits conversations and task checkpoints into Durable Object SQLite. Workspace-changing tools and delivery operations publish a filesystem snapshot before returning their result to Pi. A restarted issue host destroys any surviving container before restoring the last published snapshot. This prevents a surviving command from racing resumed work. Interrupted unsafe tools are reported by Pi rather than replayed automatically.

Publication uses a stable branch and PR marker. If GitHub creates the PR before Pi commits its checkpoint, recovery looks up and validates that PR before proceeding. Completion first persists the job outcome, then closes Pi, destroys the sandbox, and records cleanup success. Cleanup errors remain visible and retain capacity for retry. Terminal snapshots are retained for inspection; automatic snapshot/history retention and deletion are not implemented.

Commands have a 90-second deadline and output limits of 4 MiB per stream. Cancellation destroys the sandbox to stop descendant processes. Container activity is supervised by 15-second alarms with a 120-second inactivity timeout. Native container snapshots and the Durable Object scheduling policy are beta Cloudflare features.

## Validation limits

The recovery suite runs the real Workers runtime, SQLite-backed Durable Objects, Pi Durable, Docker, shell commands, and Git. It uses Pi's scripted model provider and a persistent test GitHub port. A separate local Wrangler check exercises the production container adapter and native filesystem snapshot restoration.

Live model calls, real GitHub installation-token authorization and PR mutations, and a deployed Cloudflare failure trial are not covered. Local engine checks establish local compatibility; they do not establish hosted reliability or billing behavior.

Detailed check results and regression evidence are recorded in [VALIDATION.md](VALIDATION.md).
