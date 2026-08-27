# Harness structured-output fixtures

These fixtures are reduced recordings from the CLI versions named in each filename. They preserve the transport shape and event ordering that the adapters consume. Conversation identifiers, assistant text, timing, usage, and cost values are stable test values.

The recordings came from the structured modes used by romp:

- `claude -p --output-format json` on Claude Code 2.1.235
- `codex exec --json` on Codex CLI 0.147.0
- `opencode run --format json` on OpenCode 1.18.18

Tests replay these files through local fake executables. They never make live agent calls.
