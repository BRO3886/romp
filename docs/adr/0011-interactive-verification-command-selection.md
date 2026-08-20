# Verification commands: user-selected ordered commands with file-backed discovery

Status: accepted

## Context

Romp must independently run the verification contract before it opens a pull request, but it cannot know which project command expresses that contract. A repository can expose the command through a Makefile target, a package manifest, a language manifest, or a CI workflow, and project authors can use arbitrary names. A file-presence heuristic such as preferring `make test` over `npm test` can select the wrong command or select a command that does not exist.

The existing configuration models verification as three named fields (`build`, `test`, and `lint`), while the runner already executes an ordered `[]string`. That split prevents `romp init` from representing the user's actual selection order and makes the configuration invent categories that the project may not use.

## Decision

The canonical configuration is an ordered command list:

```toml
[verify]
commands = [
  "make test",
  "make lint",
]
```

`romp init` leaves an existing `romp.toml` untouched. When it creates a new file in an interactive terminal, it reads project files to collect candidate commands and presents them in a filterable input-and-list interface. The user can select discovered commands or type arbitrary commands. Enter adds a candidate or typed value, an empty Enter finishes, Tab accepts the current completion, and Up/Down navigates suggestions. The order in which the user adds commands is the execution order. Exact duplicate commands, after trimming outer whitespace, are rejected.

Discovery is advisory. Romp limits discovery to project configuration files, not command names, and does not claim that a command is valid, available, or correct. It enumerates Makefile targets and package manifest scripts, derives commands from language manifests, includes exact commands found in CI workflows, merges duplicate commands, and displays the source references. It does not inspect README files or other Markdown documentation, execute or expand wrappers, or run a verification preflight during initialization.

`--verify` is repeatable. In an interactive terminal, supplied flags preselect commands and the user may add more. In a non-interactive terminal, explicit `--verify` flags are required; Romp never guesses. If the user finishes without commands, `init` writes no configuration and performs no label or `.gitignore` changes. An existing `romp.toml` remains untouched even when flags are supplied.

The legacy `build`, `test`, and `lint` fields are removed because Romp is pre-alpha. The runner executes `verify.commands` sequentially and stops at the first failed command.

## Consequences

- The configuration mirrors the runtime contract and preserves user-selected ordering.
- Initialization gives project evidence without taking ownership of command correctness.
- Arbitrary project naming remains discoverable because target and script names are not filtered.
- Candidate lists can be large and CI commands can be stale; the user remains responsible for the final selection.
- Existing pre-alpha configuration files require migration to `verify.commands`.
- Interactive initialization adds a terminal UI dependency, while scripted initialization remains available through repeatable flags.
