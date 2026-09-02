# Existing GitHub issues

Use this mode when the input identifies an existing issue by URL, number, or `owner/repo#number`.

## Read before rewriting

Read the current title, body, comments, labels, assignees, and URL. Treat comments as decision history, not disposable discussion. Inspect linked issues or pull requests when they constrain scope or ordering. Then inspect the relevant repository code and verification setup.

Use `gh issue view` with structured JSON where available. Do not rely on the rendered terminal view when exact body or comment text matters.

Preserve labels, assignments, milestones, links, and comments. Change the title only when it contradicts or obscures the resolved outcome. Never discard a requirement merely because it appears only in a comment.

## Authorization boundary

- `audit`, `check`, `draft`, or `is this rompified?` is read-only. Return the verdict and proposed title/body diff.
- `rompify`, `rewrite`, or `update` an identified issue authorizes changing its title or body after the contract passes the readiness checks.
- A free-text request does not authorize creating a GitHub issue. Create one only when the user explicitly asks.
- If a product decision remains, do not mutate the issue. Return **BLOCKED** and ask the narrow question.

## Safe update

Write the complete proposed body to a temporary file and pass it with `gh issue edit --body-file`. Do not pass Markdown inline through the shell. Supply `--title` only when the title must change.

After an update, fetch the issue again and compare the stored title and body with the intended contract. Report the issue URL, the verified changes, and any preserved unresolved discussion. Do not claim success from the edit command alone.
