# Experimental v2 validation

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
