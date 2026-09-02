---
title: "Documentation"
description: "Documentation for romp: getting started, command reference, configuration, the goal contract, outcomes, architecture, and safety."
weight: 1
---

## Documentation

romp is an opinionated runner for the coding agent you already have. It makes a
handful of calls on your behalf — a goal contract instead of a task description,
verification romp runs itself rather than trusting the agent, a review gate on
every diff, and a hard stop before merge — and it does not offer a switch to
unmake them. The pages below describe those choices and how to work with them.

- [Getting Started](/docs/getting-started/) — Requirements, installation, and your first labelled issue
- [Commands](/docs/commands/) — Reference for every command and flag
- [Configuration](/docs/configuration/) — `romp.toml`, config layering, and defaults
- [Goal Contract](/docs/goal-contract/) — The prompt romp hands your agent
- [Outcomes](/docs/outcomes/) — The terminal state of every job
- [Architecture](/docs/architecture/) — How romp works under the hood
- [Safety](/docs/safety/) — What to know before you point it at a repo
