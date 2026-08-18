// Package harness defines the interface romp uses to drive a coding agent.
// A harness runs a rendered goal prompt in a worktree and returns what the
// agent produced. Adapters (claude, codex, ...) implement this interface.
package harness

import "context"

// Request is a single agent run.
type Request struct {
	// Dir is the working directory the agent runs in (the worktree).
	Dir string
	// Prompt is the rendered goal contract.
	Prompt string
	// Model, when non-empty, requests a specific model; empty uses the
	// harness default.
	Model string
	// Effort, when non-empty, is a thinking budget already accepted for
	// this harness at config load (claude --effort, Codex
	// model_reasoning_effort). Empty leaves the harness default in place.
	Effort string
	// MaxTurns, when positive, caps the agent's turn budget (claude's
	// --max-turns). Codex has no equivalent and ignores this field.
	// Zero leaves the harness default in place.
	MaxTurns int
}

// Result is the outcome of a run. Fields are added additively so adapters
// stay source-compatible.
type Result struct {
	// Output is the agent's final text output, if any.
	Output string
}

// Harness drives a coding agent. Implementations are expected to block until
// the agent exits and to return a non-nil error if the run itself failed
// (e.g. the CLI could not start), as opposed to the agent declining the work.
type Harness interface {
	Name() string
	Run(ctx context.Context, req Request) (Result, error)
	// Check verifies the adapter's CLI is installed and healthy, returning a
	// short human-readable detail (e.g. a version) or an error with an
	// actionable message. doctor calls it as a preflight; it must not run the
	// agent.
	Check(ctx context.Context) (detail string, err error)
}
