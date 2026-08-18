# romp re-runs the test command itself

The agent's own claim that tests pass is not proof, so after the agent exits, romp runs the verify command again itself in the worktree before opening a PR — no green, no PR. This exists because an agent can believe tests pass, or not run them, and still report success; trusting the author means the cheapest green wins.
