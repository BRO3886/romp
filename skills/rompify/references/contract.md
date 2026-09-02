# Rompified issue contract

A rompified issue is a deterministic execution contract compiled from user intent and repository evidence.

## Required body

Use this order. Omit no section. Write `None` only when repository inspection proves the section has no entries.

```markdown
## Outcome

<One observable end state.>

## Repository evidence

- `<path or symbol>`: <fact that constrains this issue>

## Scope

### In scope

- <Exact component, package, command, API, schema, or workflow>

### Out of scope

- <Explicit adjacent behavior that must not change>

## Dependencies and consumers

- <Prerequisite, caller, reader, job, configuration consumer, or downstream system>

## Requirements

1. **R1:** The system MUST <observable behavior>.
2. **R2:** WHEN <specific condition>, the system MUST <observable result>.
3. **R3:** The system MUST NOT <forbidden behavior>.

## Acceptance criteria

- [ ] **AC1 (R1):** GIVEN <state>, WHEN <action>, THEN <observable result>.
- [ ] **AC2 (R2, R3):** GIVEN <state>, WHEN <action>, THEN <observable result and preserved invariant>.

## Verification

- `<exact repository command>`
- <Targeted test or observation that proves specific acceptance criteria>
```

The issue title must name the result, not the activity. Prefer “Reject expired session tokens during refresh” over “Improve authentication.”

## Controlled vocabulary

Use these terms for normative clauses:

- **MUST** for required behavior.
- **MUST NOT** for forbidden behavior.
- **WHEN ... THEN ...** for a trigger and its observable consequence.
- **ONLY** and **EXACTLY** when a boundary or cardinality is load-bearing.
- **OUT OF SCOPE** for adjacent work the builder must not include.
- **VERIFIED BY** when a requirement needs an unusual oracle.

Do not use `SHOULD`, `MAY`, `could`, `ideally`, `as needed`, `appropriate`, `etc.`, `TBD`, or unresolved alternatives. Replace verbs such as `improve`, `support`, `handle`, `clean up`, and `make robust` with an observable input, state transition, output, or failure.

## Decision rules

- Resolve technical placement and naming from established repository conventions when one convention clearly applies.
- Keep product semantics, compatibility policy, permissions, data retention, and user-visible tradeoffs with the user.
- Do not list options in the final issue. Ask the user first when no repository fact selects one.
- Do not prescribe exact files when only an area is known. Name the narrowest verified package, component, command, API, schema, or workflow instead.
- Do not invent verification commands. Read them from repository configuration, manifests, Makefiles, or documented contributor workflows.
- Use expected values from the user request, current behavior, authoritative documentation, schemas, or observed engine output. Do not derive them from a proposed implementation.

## Readiness checks

Return **ROMPIFIED** only when all checks pass:

1. Every domain noun resolves to a named repository or product boundary.
2. Every normative verb ends in an observable result or preserved invariant.
3. Every `MUST` and `MUST NOT` maps to at least one acceptance criterion.
4. Every acceptance criterion has a verification command, targeted test, or named observation.
5. The scope names the expected change area and all verified consumers.
6. Dependencies and ordering constraints are explicit.
7. No product decision, contradictory clause, option menu, or placeholder remains.
8. Two competent builders would ship the same observable behavior and boundaries.

If any check fails, return **BLOCKED** and list only the missing evidence or decisions. Do not soften the contract to force a pass.
