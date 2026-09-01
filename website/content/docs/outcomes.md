---
title: "Outcomes"
description: "The terminal state of every romp job."
weight: 6
---

Every job ends in exactly one named outcome. There is no silent grey area.

| Outcome | What you get |
| --- | --- |
| **done** | PR opened, trigger label removed. |
| **blocked** | No PR. `romp:blocked` label + gap comment. |
| **no-changes** | Agent exited clean with no commits. No PR. |
| **changes-requested** | Verification passed, but blocking review findings remained after one fix round. The PR and worktree stay available with both review-pass comments. |
| **red** | Verify failed on independent re-run. No PR, worktree kept. |
| **timeout** | Exceeded `timeout`. Killed, worktree kept. |
| **cancelled** | You cancelled. Worktree, branch, and both labels removed. |
| **error** | git/gh failure (incl. rate limits outliving retries). Re-claimed later. |

`rate-limited` is not an outcome: it names the in-job GitHub retry, not a
terminal state.

## Claim and release

On claim, romp performs three coordinated writes:

1. Inserts a job row with a `UNIQUE(repo, issue)` constraint — a losing insert
   skips, serializing watchers on the same machine.
2. Adds the claim label — the cross-machine signal, since two teammates don't
   share a state dir.
3. Assigns the authenticated GitHub user (`@me`) so the issue shows who is
   working it.

On release — done, blocked, cancel, and every other terminal state — romp
removes the claim label and unassigns `@me`.

## Red and timeout keep the worktree

`red` and `timeout` leave the worktree (and the `romp-N` branch) in place so
you can inspect what the agent produced. `cancel` is the opposite: a full
cleanup, because it was your call. `gc` reclaims leftover worktrees.

## Cancelling abandons

`romp cancel` is an abandon, not a restart. It removes the trigger label too,
so the next poll does not re-run the issue. To retry, re-label the issue by
hand.
