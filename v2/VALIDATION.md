# Refactor validation

Validated locally on macOS on October 14, 2026. The compiler, linter, and default test run use Node 25.8.1. The behavior suite also runs against the CI runtime, Node 22.22.1.

## Current checks

- `npm ci --ignore-scripts` restores the locked dependencies.
- `npm run check` builds the application and typechecks source and tests with the strict compiler options.
- `npm run lint` checks types, promise handling, unchecked values, and architecture import boundaries.
- `npm run format:check` checks the repository's formatting rules.
- The behavior suite now includes 22 tests, retaining the original 17 cases and adding protocol, checkpoint-invariant, legacy SQLite migration, and inherited-property regressions.
- `npm audit` reports zero vulnerabilities in the complete dependency graph.

The migration also passed against a checkpoint produced by the actual runtime at commit `9e4c797`. That worker was killed after the test GitHub port created the PR. The refactored runtime migrated the task from version 1 to version 2, reused the PR, ran only the reviewer, and finished successfully.

## New regressions

The original socket client accepted a malformed successful ping result containing a numeric version. The regression was reproduced against the original commit:

```text
RED original 9e4c797: Missing expected rejection for malformed ping result
GREEN refactor: malformed ping result rejected
```

The new client parses the response envelope and the selected method's result schema. A permanent test exercises this through an actual Unix socket.

Job lookup also treated inherited JavaScript properties as stored jobs. Calling `job('toString')` returned a function. The regression test failed before the repair:

```text
job lookup rejects inherited dictionary properties
AssertionError: Missing expected rejection.
```

The shared dictionary lookup now requires an own property. The application lookup and Pi state readers use it. The regression checks `toString`, `constructor`, and `__proto__`.

## Coverage limits

Live model calls, account-specific OAuth login, hosted GitHub mutations, and Cloudflare deployment have not been exercised. The local suite uses the real Pi runtime, SQLite, git, processes, and sockets with scripted model replies and the test GitHub port. Hosted CI status must be checked after pushing the refactor.

The following record preserves the initial experiment's evidence.

---

# Initial experiment validation

Validated locally on macOS on October 13, 2026, using Node 22.22.1 and Pi Durable 1.2.0.

## Passed

- `npm ci --ignore-scripts` restored the locked dependency graph.
- `npm run check` passed TypeScript's strict checks.
- `npm test` built the compiled application and passed all 17 tests.
- `npm audit --omit=dev` reported zero vulnerabilities.
- `git diff --cached --check` found no whitespace errors.
- The installed GitHub CLI's read-only authentication, repository identity, and paginated issue lookup worked against `BRO3886/romp`.

The tests exercise real Pi Durable scheduling and storage, Node SQLite, coding tools, local git worktrees and bare remotes, shell processes, and Unix sockets. Model responses come from Pi's scripted provider; GitHub mutations use an application-port fixture.

The suite covers PR artifact validation, independent verification, repair and re-verification, reviewer mutation denial, malformed review output, exhausted repair budgets, empty diffs, global admission, persisted timeouts, cancellation, daemon ownership and reopen, credential serialization, and compiled CLI execution through a bin symlink.

Two recovery tests kill an actual worker with SIGKILL. One kills it after the test GitHub port has created a PR but before the runtime checkpoints the URL. Reopen reuses that PR and runs only the reviewer. The other kills it during a running shell tool. Startup stops the previous supervised process group before the agent continues.

## Failures found and repaired

The initial end-to-end test failed with:

```text
Agent completed without a final text report
'failed' !== 'done'
```

The parent task's entry accessor could not see the child conversation's answer. Reading the typed answer through the transaction entry API fixed the boundary. The end-to-end, verification-failure, repair, and read-only tests then reached their intended assertions.

The cleanup regression failed with:

```text
Missing expected rejection
```

Git reported the canonical `/private/var` worktree path while the job recorded its `/var` alias. Canonicalizing the state directory before constructing job paths fixed cleanup and recovery identity. The successful-job test now verifies that the worktree is absent.

The first dependency audit found a high-severity advisory in the initially selected TOML parser version. The lockfile now pins `smol-toml` 1.9.1, and the final audit reports zero vulnerabilities.

## Coverage gaps

- Hosted CI has not run; the workflow is committed for future branch or PR runs.
- Coverage gap: the hosted GitHub mutation engine is not runnable against the local fixture. CLI flags and read paths were checked, but real PR creation, comments, and labels were not mutated during validation.
- No live model request or account-specific OAuth login was performed. The suite establishes runtime behavior, not model coding quality, subscription access, or provider billing.
- The retained Go implementation was not changed or retested. The experiment uses a separate package and state directory.

Feature differences and operating instructions are in `README.md`.
