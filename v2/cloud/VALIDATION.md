# Cloud runtime validation

Validation performed on 14 October 2026. Local checks provisioned no Cloudflare resources and made no live model or GitHub mutation calls.

## Passed checks

- Strict source and test typechecks, type-aware linting, and formatting checks.
- The existing local runtime's 22 tests.
- Fourteen Workers tests using real Durable Object SQLite, Pi Durable, Docker, Bash, and Git: signed admission and duplicate delivery across eviction; signature, installation, action, and size rejection; persistent concurrency; queued cancellation; authenticated configuration checks; RSA key formats; successful delivery; interruption after PR creation with container loss; cleanup failure; cancellation before admission; shared concurrency across live runs; expired admission; pause/resume across eviction; and active cleanup while paused.
- Local Wrangler's production container adapter: Node 22 execution, native filesystem snapshot creation, container destruction, file restoration, and Git worktree restoration.
- Clean installation with Node 22.22.1 and npm 10.9.4 in a Linux x64 container, including generated binding checks and the preflight test. The lockfile contains every workerd platform binary; Vite is pinned to 7.3.7 to keep the test compiler graph reproducible.
- Wrangler type generation checks and a deployment dry run that builds the Worker and Linux image without deploying them.
- Nine Node setup tests covering discovered installation IDs, private credential files, idempotent label creation, deployment-readiness failures, ambiguous webhook-update recovery, invalid configuration/access gates, administration, and refreshed App authorization after a long build. The generated configuration is also passed to real Wrangler with `--dry-run --secrets-file`; secrets arrive as hidden bindings, and the Worker and Linux image build successfully. Account/GitHub HTTP responses in this suite are contract fixtures, not live API compatibility proof.

The publication recovery test creates a PR in a persistent test GitHub port, destroys the Docker workspace and aborts the Workers actor before the publication checkpoint commits, then restores the workspace and resumes from the actor's SQLite storage. It requires one PR creation and exactly two scripted model turns, proving that recovery does not repeat the builder. The GitHub port is a fault fixture; this does not prove hosted GitHub authorization or mutation behavior.

## Regression evidence

Removing the expired-admission guard reproduced this failure:

```text
FAIL expired admission stops before model or workspace provisioning
AssertionError: expected { id: 'r123-i41', ... } to be undefined
```

The unguarded host created a durable job input and workspace after its deadline had already passed. Restoring the guard made the same test pass. The full cloud suite then passed with the guard in place.

The first container-loss fixture restored files with the host's UID. Git rejected the restored worktree with `fatal: detected dubious ownership`. The Docker fixture now restores the image's root ownership. The native snapshot check independently confirms that Cloudflare's local snapshot engine restores a usable Git worktree; the production host does not disable Git ownership checks.

The new pause endpoint test first failed with `expected 404 to be 200`. After adding persisted pause state and gating new scheduler grants, it passed in workerd. Another test advanced the clock through a long image build and failed with `GitHub App JWT must not expire during an image build`. Refreshing App authorization before webhook update and verification made that test pass.

## Coverage gaps

- Live provider inference and account-specific model access.
- GitHub App installation-token authorization, real repository pushes, and hosted PR mutations.
- Deployed Cloudflare eviction, container loss, capacity limits, billing, and beta snapshot behavior.
- Live setup deployment, GitHub App hook changes, label mutations, and actual GitHub-to-Worker event delivery. App registration, webhook activation, and installation remain manual prerequisites.

The local container check uses the real Wrangler/workerd container engine. The Workers test plugin does not expose `ctx.container`, so the combined Pi recovery suite uses a real Docker workspace through an injected execution port. Both checks are required; neither substitutes for a deployed failure trial.

## Subscription authentication, 15 October 2026

The subscription path was checked with strict source/test typechecks, type-aware lint, formatting, generated binding verification, the 25 local runtime tests, 20 Workers tests, 13 setup tests, and the lower-level preflight test. Local Wrangler also built both generated API-key and subscription configurations with private secret files and no deployment. The native container execution/snapshot check and the production deployment bundle passed. Production dependency audit reported no vulnerabilities.

The six added Workers tests cover subscription configuration without an API key; encrypted credential persistence; eight concurrent model-authentication callers sharing one refresh; a second refresh using the rotated token after actor eviction; replayed upload acknowledgements; interrupted/rejected/redirected refreshes without API-key fallback; host identity and permission-scope validation; authenticated Worker doctor/import/status operations; session revocation with confirmed and unavailable responses; and rejection of old uploads after replacement or logout. The interrupted-refresh fixture restores a persisted in-flight marker after eviction. It verifies conservative recovery from that state; it does not reproduce a hosted crash during an actual OpenAI exchange.

A delivery test uses Pi's real OpenAI Responses adapter, Pi Durable, Workers SQLite, Docker, Bash, and Git. HTTP fixtures supply an OAuth refresh response and two streamed Responses API turns. Assertions require the refreshed bearer token, streamed/stateless request parameters, exclusion of unsupported subscription parameters and system-role input, successful verification/publication, and absence of access/refresh tokens from the issue actor's application tables and key-value records. OpenAI HTTP fixtures are contract checks, not live service compatibility or entitlement proof. Test HTTP handlers reject unexpected external requests and allow the local Docker bridge.

Setup tests exercise dedicated login-state storage and stable host identity, private pending files, no transfer during a dry run, successful handoff before webhook activation, local renewable-copy removal, unchanged cloud credentials on redeploy, recovery from a lost upload response using the same session ID, pending-session revocation before deployment, and older profiles defaulting to API-key mode. Browser interactions and account credentials are injected in setup tests; interactive sign-in was not performed.

The original configuration rejected the subscription-only environment with:

```text
Error: Missing API key for openai
```

The new configuration test accepts that environment and rejects a missing encryption key. The real Workers test also exposed an invalid redirect policy before the fix:

```text
TypeError: Invalid redirect value, must be one of "follow" or "manual"
```

OAuth requests now use manual redirect handling and require the expected success status. A redirect to another origin fails authentication without forwarding the refresh token. Completed OAuth requests clear their deadline timers so actor eviction does not wait for an unused timeout.

Coverage gap: live OpenAI browser sign-in and subscription inference were not runnable without the user's interactive authorization. No Cloudflare deployment, real GitHub webhook change, hosted PR mutation, or model-billing trial was performed. The published `v2.0.0-experimental.1` installer bundle does not contain this feature; the current `v2` branch does.

A setup regression test also failed with `actual: undefined` when switching a subscription profile to API-key mode discarded its saved encryption key. Resolution now retains an existing key across authentication-mode changes, and the same test passes. This prevents a later deployment from silently generating a different key for the same credential owner.
