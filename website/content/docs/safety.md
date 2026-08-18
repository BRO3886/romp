---
title: "Safety"
description: "What to know before you point romp at a repo."
weight: 8
---

romp runs a coding agent on your machine. That agent can run arbitrary tools
and edit files. Read this before you let it loose.

## The label is privileged

The agent is driven by text that anyone with repo write access can author —
the issue title and body. Adding the trigger label to an issue is authorizing
your machine to execute whatever that issue says. Treat the trigger label as a
privileged action and label only issues you would run yourself.

## romp never merges

romp opens the pull request and stops. It does not merge, deploy, or otherwise
touch production. Review every PR before merging. Branch protection on the
default branch is strongly recommended.

## No secrets in the tree

Do not run romp against a repo with production credentials or secrets checked
in. The agent can read the whole tree. Scope with `[scope] ignore` if you must
keep sensitive files nearby.

## Sandboxing is the harness's

romp does not add its own sandbox. It relies on whatever the harness provides:

- Codex runs with `--sandbox workspace-write`
- Claude runs with `--permission-mode bypassPermissions`

Neither is a substitute for not putting secrets in the repo.

## Pre-alpha

romp is pre-alpha. Breaking changes are expected. Do not point it at a repo
you can't afford a bad branch on.

## Non-goals

romp is not interactive steering, live collaboration, cloud execution,
merging, or deploying. It is not a general agent framework. Use your agent CLI
directly, or a hosted service, for those.
