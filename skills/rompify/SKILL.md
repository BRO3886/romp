---
name: rompify
description: Convert a short product request or an existing GitHub issue into a deterministic, repository-grounded execution contract that Romp can accept. Use when asked to rompify, scope, make pickup-ready, audit, or rewrite an engineering issue. Do not use to implement the issue.
---

# Rompify

Turn user intent into a closed execution contract. The result must tell a builder what observable behavior to ship without asking it to make product decisions.

## Select the input mode

- For free text, treat the text as intent rather than as the final issue.
- For an existing GitHub issue, read [references/github.md](references/github.md) before using `gh` or changing the issue.
- For both modes, read [references/contract.md](references/contract.md) before drafting the contract.

## Ground the request

Inspect the repository before writing. Read its agent instructions, relevant architecture, implementation, callers, tests, configuration, and verification setup. Resolve each user-facing noun to a real code or product boundary. Enumerate the consumers of every shared API, schema, configuration value, or behavior that the issue can change.

Keep these sources distinct:

- **User requirements** define the desired product behavior.
- **Repository evidence** establishes current behavior, seams, constraints, and verification commands.
- **Technical decisions** may follow established repository conventions when those conventions determine one answer.

Do not infer a missing product decision from code style or personal preference. Ask one narrow question when different answers would produce different user-visible behavior, compatibility, data, permissions, or scope. Do not call the issue rompified while such a decision remains.

## Compile the contract

Use the controlled structure and vocabulary in [references/contract.md](references/contract.md). Replace broad prose with observable clauses. Fix one choice where the input presents implementation-relevant alternatives. Preserve the user's intent and remove accidental scope expansion.

Return one of two verdicts:

- **ROMPIFIED** when the contract passes every readiness check.
- **BLOCKED** with the smallest set of unresolved product decisions or missing evidence needed to continue.

Do not implement the issue, run Romp, create a branch or pull request, or create a new GitHub issue unless the user separately requests that action.
