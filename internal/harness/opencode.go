package harness

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// OpenCode drives the OpenCode CLI in non-interactive run mode.
type OpenCode struct {
	Args []string
}

func (o OpenCode) Name() string { return "opencode" }

func (o OpenCode) Check(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("opencode"); err != nil {
		return "", fmt.Errorf("opencode CLI not found on PATH: install OpenCode and log in")
	}
	ver, err := exec.CommandContext(ctx, "opencode", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("opencode --version failed: %w\n%s", err, ver)
	}
	return strings.TrimSpace(string(ver)), nil
}

func (o OpenCode) Run(ctx context.Context, req Request) (Result, error) {
	cmd := exec.CommandContext(ctx, "opencode", opencodeArgs(req, o.Args)...)
	cmd.Dir = req.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Output: string(out)}, fmt.Errorf("opencode: %w\n%s", err, out)
	}
	return Result{Output: string(out)}, nil
}

func opencodeArgs(req Request, extra []string) []string {
	args := []string{"run", "--dangerously-skip-permissions"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, extra...)
	return append(args, req.Prompt)
}
