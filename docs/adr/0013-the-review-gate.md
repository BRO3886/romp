# The review gate

Status: accepted

## Context

The done contract (ADR 0001) is verify passes plus an opened PR. Verify proves the code works; nothing checks that the change is correct, complete against the issue, or meets repo standards. A builder grading its own diff is self-agreement: the same assumption written twice, in one context window.

The original gate sent every routed lens to one reviewer. Production history showed that this encouraged early stopping and diluted attention across five to seven concerns. Later passes then found defects that already existed in the first diff. Fix builders also reported test and static-analysis work that the next reviewer could not see, so it sometimes repeated already completed requests.

## Decision

### Gate placement

After verify passes, the builder commit is pushed and the PR is opened before review starts. The PR is the durable review record and the branch is the shared target for any fix round.

### Durable review record

Every successfully parsed pass is posted as a new ordinary PR comment before the next pipeline action. The comment includes its pass number, verdict, and all findings grouped by severity, including file and line locations. An approval with no findings says so explicitly. A fix round never edits or replaces the first pass comment.

A harness or parsing failure is not a finding. Romp leaves the PR and worktree in place and posts a distinct comment that says the review did not complete. If PR creation or review-comment publication fails, the pipeline stops before any later reviewer or builder run.

### Focused lens fan-out

Romp runs one independent read-only harness invocation for every lens in the deterministic review plan. The invocations run concurrently within the job deadline. Each invocation receives:

- the diff against base and the changed-file list;
- one focused lens selected from the changed-file plan (path-token classification adds security scrutiny when paths touch auth, crypto, or secrets, and language conventions come from file types);
- discovered repo review skills and conventions as references;
- verify results;
- reports from every builder run;
- all prior review outcomes during re-review;
- the issue title and body — the reviewer judges **spec compliance** (is this complete against what the issue asked), not just code mistakes;
- the artifact contract below.

The prompt requires each lens to inspect every relevant changed file and not stop after its first finding. A re-review must check prior findings and also perform a fresh sweep, because a fix can introduce a different defect. Builder reports provide evidence for report-only requirements such as showing a RED run, but they do not replace Romp's independent verification transcript.

Synthesis is mechanical in Go. Romp preserves lens-plan order, deduplicates identical file, line, and description tuples, and promotes duplicates to the highest reported severity. Any blocking finding triggers the fix round. Otherwise the job proceeds. Romp posts only the merged pass to the PR, not one comment per lens.

The pass is all-or-nothing. A harness failure, panic, invalid result, or deadline in any lens cancels the sibling invocations and fails the entire pass. Romp never treats a partial set of lens results as approval.

### Read-only enforcement

`harness.Request` gains a `ReadOnly bool`. Each adapter maps it to its native mechanism. A prompt instruction is not enforcement: a reviewer holding write access can mutate what it reviews.

Verified empirically on the installed CLIs:

- **claude (2.1.235): only deny rules enforce.** Under `bypassPermissions`, `--disallowedTools` blocks the named tools (a Write attempt was refused), but `--allowedTools` allowlists are ignored — a Bash write outside the allowlist executed with empty `permission_denials`, and the same happened under the default headless permission mode. So claude's reviewer posture is `bypassPermissions` plus deny rules: `--disallowedTools "Write,Edit,NotebookEdit,Bash"`. This costs the reviewer shell access, so romp bakes the diff and recent history into the goal contract instead; Read, Grep, and Glob remain available for code hunting.
- **codex (0.147.0):** `exec --sandbox read-only` exists and is the natural mapping; romp's builder explicitly sets `workspace-write`, so ReadOnly selects read-only instead.
- **opencode (1.18.18):** romp passes `run --auto`, described by the CLI itself as "auto-approve permissions that are not explicitly denied". Enforcement is therefore also deny-based: ReadOnly maps to a read-only permission config or dedicated agent with edit and bash denied.

The common shape: every harness enforces explicit denials, none enforces allowlists under their permissive modes. ReadOnly means "deny mutation everywhere," never "allow only reading."

### Artifact contract

The reviewer returns one strict JSON document as `harness.Result.Output`; it writes no review file. The document contains a verdict (`approve` | `fix`) and findings with severities (`blocking`, `non-blocking`, `nit`). A successful harness exit with empty, malformed, or semantically invalid output is an error, never an implicit approve. One or more blocking findings under `fix` make the fix round mandatory. An `approve` verdict may contain non-blocking findings and nits, but never a blocking finding.

The runner supplies the complete diff, branch log, changed files, focused lens, verification transcript, builder reports, prior outcomes, and convention references to each review prompt. It parses every `harness.Result.Output`, merges the typed outcomes, and consumes one logical pass. The renderer, parser, and outcome merger remain pure: they do not discover inputs, inspect configuration, invoke a harness, or access the filesystem.

### Fix round

Two rounds by default, configurable through `review.max_fix_rounds`: each round runs a fresh builder in the same worktree, followed by re-verification (the previous green is stale), push to the PR branch, and re-review. A round normally embeds the current blocking findings as constraints. If re-verification fails and another round remains, the next fresh builder also receives the failed command, exit code, and captured output as independent evidence. A failed re-verification consumes its round; the job becomes `red` only when no repair round remains. A zero budget records `changes-requested` after the first blocking review. Session resume is available only behind the undocumented `ROMP_FIX_MODE=resume` for experimentation; adjacent evidence (weak intrinsic self-correction, context rot, Reflexion's distilled-feedback retries) favors fresh context with findings embedded.

On exhausted rounds the job records `changes-requested`: distinct from red because verify passed — the code works but does not meet the bar.

### Worktree and branch cleanup

The worktree's inspection value exists only while its contents match what the gates actually judged. Cleanup follows the terminal outcome:

- **Removed** when the pipeline completes: any route to `done` (clean approve, approve with nits, docs-only skip, or a fix round ending in an approved re-review), plus `blocked`. Also removed on `no-changes`, which today leaks the directory; since such jobs never committed, the zero-commit `romp-N` branch ref is deleted with it.
- **Kept** whenever a human needs to inspect what failed: `red` (the tree is exactly what failed verify), `changes-requested` (the final tree equals what the reviewer rejected, even though the fixer mutated it mid-job), `timeout`, `interrupted`, and errors including empty or invalid reviewer output.

Branches are separate from worktrees and follow their own rule: `done` keeps the branch (it is the PR head), `blocked` and `no-changes` delete it, everything kept-for-inspection keeps both.

Worktrees accumulate across kept outcomes, so gc planning (ADR 0010) eventually owns reclaiming them; until then removal discipline above bounds growth to failure paths only.

### Configurability

Per-repo on/off and maximum fix rounds in `romp.toml`, which governs watch mode; `romp run` takes its own flag to skip the gate for one-shot runs. Separate `review.model` defaulting to the builder's model. Same-harness reviewer by default; cross-harness review (`review.harness`) is allowed. Docs-only diffs skip agentic review entirely — no code files means nothing for the gate to judge beyond what verify covers. Disabled and docs-only runs still open the PR, but post no review-pass comment because no review ran.

Session IDs (ADR 0012) are a prerequisite only for the experimental resume path, not for the gate itself.

## Consequences

- Every successful code job passed two independent gates: mechanical verification and adversarial review. A PR can remain open when review fails or requests unresolved changes, with the durable pass record showing why.
- Review compute grows with the routed lens count. The invocations run concurrently, so pass latency is approximately the slowest lens rather than the sum of all lenses, but each lens still consumes a separate harness call. Each blocking fix round adds one builder run plus another full lens fan-out.
- The outcome taxonomy grows by `changes-requested`; labels grow to match.
- Lens routing lives in Go, so improving review emphasis is ordinary code, not prompt archaeology.
- Reviewer calibration is now load-bearing: a noisy reviewer burns fix rounds, a quiet one waves bad code through. Findings rates, aggregate pass duration, lens count, and re-verification failure count belong in instrumentation from day one.
