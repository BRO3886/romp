package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Codex drives the Codex CLI (codex exec) in non-interactive mode.
type Codex struct {
	// Args are extra arguments passed to `codex exec`, e.g. ["--profile", "romp"].
	// The sandbox defaults to workspace-write so the agent can edit the worktree
	// without a TTY; override via Args if you want to tighten or loosen this.
	Args []string
}

func (c Codex) Name() string { return "codex" }

// Check verifies the Codex CLI is installed and healthy. It reports the
// installed version but stops short of proving a live agent call: `codex
// doctor` validates installation and auth, and a preflight must not start a run.
func (c Codex) Check(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return "", fmt.Errorf("codex CLI not found on PATH: install Codex and log in")
	}
	ver, err := exec.CommandContext(ctx, "codex", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codex --version failed: %w\n%s", err, ver)
	}
	out, err := exec.CommandContext(ctx, "codex", "doctor").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codex doctor reported a problem: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(ver)), nil
}

func (c Codex) Run(ctx context.Context, req Request) (Result, error) {
	cmd := exec.CommandContext(ctx, "codex", codexArgs(req, c.Args)...)
	cmd.Dir = req.Dir
	// CombinedOutput leaves stdin on /dev/null. Codex treats a non-TTY
	// stdin as piped input and appends it to an argv prompt, so the
	// goal contract goes on stdin and stays off argv.
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result, parseErr := parseCodexResult(stdout.Bytes())
	if err != nil {
		return result, diagnosticError("codex", err, stdout.Bytes(), stderr.Bytes())
	}
	if parseErr != nil {
		return Result{}, diagnosticError("codex", fmt.Errorf("parsing structured output: %w", parseErr), stdout.Bytes(), stderr.Bytes())
	}
	return result, nil
}

func parseCodexResult(out []byte) (Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(out))
	var result Result
	for {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return Result{}, err
		}
		switch {
		case event.Type == "thread.started":
			result.SessionID = event.ThreadID
		case event.Type == "item.completed" && event.Item.Type == "agent_message":
			result.Output = event.Item.Text
		}
	}
	if result.SessionID == "" {
		return Result{}, fmt.Errorf("event stream has no thread.started.thread_id")
	}
	return result, nil
}

func codexArgs(req Request, extra []string) []string {
	args := []string{"exec", "--json", "--sandbox", "workspace-write", "--color", "never"}
	if req.Dir != "" {
		args = append(args, "--cd", req.Dir)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+req.Effort)
	}
	args = append(args, extra...)
	return args
}
