package harness

import (
	"context"
	"fmt"
	"os/exec"
)

// Claude drives the Claude Code CLI (claude) in non-interactive print mode.
type Claude struct {
	// Args are extra arguments passed to claude, e.g. ["--model", "sonnet"].
	// The permission mode defaults to bypassPermissions so the agent can edit
	// files and run commands without a TTY; override via Args if you want to
	// tighten this.
	Args []string
}

func (c Claude) Name() string { return "claude" }

func (c Claude) Run(ctx context.Context, req Request) (Result, error) {
	cmd := exec.CommandContext(ctx, "claude", claudeArgs(req, c.Args)...)
	cmd.Dir = req.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Output: string(out)}, fmt.Errorf("claude: %w\n%s", err, out)
	}
	return Result{Output: string(out)}, nil
}

func claudeArgs(req Request, extra []string) []string {
	args := []string{"-p", "--output-format", "text", "--permission-mode", "bypassPermissions"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, extra...)
	args = append(args, req.Prompt)
	return args
}
