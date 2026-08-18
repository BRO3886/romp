# The agent reports outcomes through files in .romp/

The agent communicates structured outcomes — a PR title, a conventional commit subject, and a markdown description (optionally with mermaid diagrams), and, when the issue is under-scoped, the specific gap — by writing markdown files under `.romp/` that romp reads after the harness exits. This is chosen over harness-specific structured output (for example claude's `--json-schema`) so the prompt is the contract and the harness interface stays language-agnostic; a codex adapter slots in without romp learning each harness's output format.
