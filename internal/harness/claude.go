package harness

import (
	"bytes"
	"context"
	"encoding/json"
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return Result{}, diagnosticError("claude", err, stdout.Bytes(), stderr.Bytes())
	}
	result, parseErr := parseClaudeResult(stdout.Bytes())
	if parseErr != nil {
		return Result{}, diagnosticError("claude", fmt.Errorf("parsing structured output: %w", parseErr), stdout.Bytes(), stderr.Bytes())
	}
	return result, nil
}

func parseClaudeResult(out []byte) (Result, error) {
	var payload struct {
		Type      string `json:"type"`
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Result{}, err
	}
	if payload.Type != "result" {
		return Result{}, fmt.Errorf("unexpected payload type %q", payload.Type)
	}
	if payload.SessionID == "" {
		return Result{}, fmt.Errorf("result payload has no session_id")
	}
	return Result{Output: payload.Result, SessionID: payload.SessionID}, nil
}

func claudeArgs(req Request, extra []string) []string {
	args := []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions"}
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
