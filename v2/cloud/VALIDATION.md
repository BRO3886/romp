# Cloud runtime validation

Validation performed on 14 October 2026. Local checks provisioned no Cloudflare resources and made no live model or GitHub mutation calls.

## Passed checks

- Strict source and test typechecks, type-aware linting, and formatting checks.
- The existing local runtime's 22 tests.
- Twelve Workers tests using real Durable Object SQLite, Pi Durable, Docker, Bash, and Git: signed admission and duplicate delivery across eviction; signature, installation, action, and size rejection; persistent concurrency; queued cancellation; authenticated configuration checks; RSA key formats; successful delivery; interruption after PR creation with container loss; cleanup failure; cancellation before admission; shared concurrency across live runs; and expired admission.
- Local Wrangler's production container adapter: Node 22 execution, native filesystem snapshot creation, container destruction, file restoration, and Git worktree restoration.
- Wrangler type generation checks and a deployment dry run that builds the Worker and Linux image without deploying them.

The publication recovery test creates a PR in a persistent test GitHub port, destroys the Docker workspace and aborts the Workers actor before the publication checkpoint commits, then restores the workspace and resumes from the actor's SQLite storage. It requires one PR creation and exactly two scripted model turns, proving that recovery does not repeat the builder. The GitHub port is a fault fixture; this does not prove hosted GitHub authorization or mutation behavior.

## Regression evidence

Removing the expired-admission guard reproduced this failure:

```text
FAIL expired admission stops before model or workspace provisioning
AssertionError: expected { id: 'r123-i41', ... } to be undefined
```

The unguarded host created a durable job input and workspace after its deadline had already passed. Restoring the guard made the same test pass. The full cloud suite then passed with the guard in place.

The first container-loss fixture restored files with the host's UID. Git rejected the restored worktree with `fatal: detected dubious ownership`. The Docker fixture now restores the image's root ownership. The native snapshot check independently confirms that Cloudflare's local snapshot engine restores a usable Git worktree; the production host does not disable Git ownership checks.

## Coverage gaps

- Live provider inference and account-specific model access.
- GitHub App installation-token authorization, real repository pushes, and hosted PR mutations.
- Deployed Cloudflare eviction, container loss, capacity limits, billing, and beta snapshot behavior.

The local container check uses the real Wrangler/workerd container engine. The Workers test plugin does not expose `ctx.container`, so the combined Pi recovery suite uses a real Docker workspace through an injected execution port. Both checks are required; neither substitutes for a deployed failure trial.
