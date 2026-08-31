// Package harness defines the interface romp uses to drive a coding agent.
// A harness runs a rendered goal prompt in a worktree and returns what the
// agent produced. Adapters (claude, codex, opencode, ...) implement this interface.
package harness

import (
	"context"
	"fmt"
	"strings"
)

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
	// this harness at config load. Empty leaves the harness default in place.
	// OpenCode passes this value as its model-specific --variant setting.
	Effort string
	// MaxTurns, when positive, caps the agent's turn budget (claude's
	// --max-turns). Codex and OpenCode have no equivalent and ignore this field.
	// Zero leaves the harness default in place.
	MaxTurns int
	// ReadOnly selects the harness's native worktree mutation restrictions.
	// The zero value preserves the writable builder behavior.
	ReadOnly bool
}

// Result is the outcome of a run. Fields are added additively so adapters
// stay source-compatible.
type Result struct {
	// Output is the agent's final text output, if any.
	Output string
	// SessionID identifies the conversation created by the harness run.
	SessionID string
}

func diagnosticError(name string, err error, stdout, stderr []byte) error {
	detail := ""
	if len(stdout) > 0 {
		detail += "\nstdout:\n" + string(stdout)
	}
	if len(stderr) > 0 {
		detail += "\nstderr:\n" + string(stderr)
	}
	return fmt.Errorf("%s: %w%s", name, err, detail)
}

func conflictingExtra(args []string, flags ...string) (string, bool) {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return flag, true
			}
		}
	}
	return "", false
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
