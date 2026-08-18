package harness

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
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

// Check verifies the claude CLI is installed and healthy. It reports the
// installed version but stops short of proving OAuth login: `claude doctor`
// validates installation, and proving the account would require a real agent
// call, which a preflight must not make.
func (c Claude) Check(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("claude CLI not found on PATH: install Claude Code and log in")
	}
	ver, err := exec.CommandContext(ctx, "claude", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude --version failed: %w\n%s", err, ver)
	}
	out, err := exec.CommandContext(ctx, "claude", "doctor").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude doctor reported a problem: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(ver)), nil
}

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
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}
	args = append(args, extra...)
	args = append(args, req.Prompt)
	return args
}
