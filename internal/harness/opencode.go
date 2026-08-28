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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return Result{}, diagnosticError("opencode", err, stdout.Bytes(), stderr.Bytes())
	}
	result, parseErr := parseOpenCodeResult(stdout.Bytes())
	if parseErr != nil {
		return Result{}, diagnosticError("opencode", fmt.Errorf("parsing structured output: %w", parseErr), stdout.Bytes(), stderr.Bytes())
	}
	return result, nil
}

func parseOpenCodeResult(out []byte) (Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(out))
	var result Result
	var textParts []string
	var completedStep bool
	for {
		var event struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionID"`
			Part      struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"part"`
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return Result{}, err
		}
		if result.SessionID == "" {
			result.SessionID = event.SessionID
		}
		if event.SessionID != "" && event.SessionID != result.SessionID {
			return Result{}, fmt.Errorf("event stream changed sessionID from %q to %q", result.SessionID, event.SessionID)
		}
		if event.Type == "text" && event.Part.Type == "text" {
			textParts = append(textParts, event.Part.Text)
		}
		if event.Type == "step_finish" {
			completedStep = true
		}
	}
	if result.SessionID == "" {
		return Result{}, fmt.Errorf("event stream has no sessionID")
	}
	result.Output = strings.Join(textParts, "\n")
	if !completedStep || strings.TrimSpace(result.Output) == "" {
		return Result{}, fmt.Errorf("event stream has no completed non-empty assistant text")
	}
	return result, nil
}

func opencodeArgs(req Request, extra []string) []string {
	args := []string{"run", "--auto", "--format", "json"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--variant", req.Effort)
	}
	args = append(args, extra...)
	return append(args, req.Prompt)
}
