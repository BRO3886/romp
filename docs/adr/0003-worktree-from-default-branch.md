# Jobs run in a worktree off origin's default branch

Each job gets a fresh `git worktree` branched from origin's default branch, not the local working tree. The base of every job is therefore deterministic and reproducible, and — deliberately — romp only ever tests code that is committed and pushed. A corollary surfaced while dogfooding: you cannot exercise uncommitted local work, so the development loop is commit → push → file issue → run → merge.
