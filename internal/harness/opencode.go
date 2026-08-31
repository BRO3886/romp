package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	openCodeConfigContentEnv = "OPENCODE_CONFIG_CONTENT"
	openCodeReadOnlyAgent    = "romp-read-only"
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
	if req.ReadOnly {
		if err := validateOpenCodeReadOnlyExtras(o.Args); err != nil {
			return Result{}, err
		}
	}

	var env []string
	if req.ReadOnly {
		var err error
		env, err = opencodeEnv(true, os.Environ())
		if err != nil {
			return Result{}, fmt.Errorf("opencode read-only environment: %w", err)
		}
		if err := verifyOpenCodeReadOnlyPolicy(ctx, req.Dir, env); err != nil {
			return Result{}, err
		}
	}

	cmd := exec.CommandContext(ctx, "opencode", opencodeArgs(req, o.Args)...)
	cmd.Dir = req.Dir
	if req.ReadOnly {
		cmd.Env = env
	}
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
	if req.ReadOnly {
		args = append(args, "--agent", openCodeReadOnlyAgent)
	}
	return append(args, req.Prompt)
}

func validateOpenCodeReadOnlyExtras(extra []string) error {
	flag, conflict := conflictingExtra(extra, "--", "--agent", "--attach")
	if conflict {
		return fmt.Errorf("opencode read-only run rejects conflicting extra argument %q", flag)
	}
	return nil
}

func opencodeEnv(readOnly bool, environ []string) ([]string, error) {
	if !readOnly {
		return environ, nil
	}

	content, found := lookupEnvironment(environ, openCodeConfigContentEnv)
	config := make(map[string]any)
	if found {
		if err := json.Unmarshal([]byte(content), &config); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", openCodeConfigContentEnv, err)
		}
		if config == nil {
			return nil, fmt.Errorf("parsing %s: top-level value must be an object", openCodeConfigContentEnv)
		}
	}

	agents, err := objectField(config, "agent")
	if err != nil {
		return nil, fmt.Errorf("merging %s: %w", openCodeConfigContentEnv, err)
	}
	agent, err := objectField(agents, openCodeReadOnlyAgent)
	if err != nil {
		return nil, fmt.Errorf("merging %s: %w", openCodeConfigContentEnv, err)
	}
	permissions, err := openCodePermissionField(agent)
	if err != nil {
		return nil, fmt.Errorf("merging %s: %w", openCodeConfigContentEnv, err)
	}

	agent["mode"] = "primary"
	permissions["edit"] = "deny"
	permissions["bash"] = "deny"
	agent["permission"] = permissions
	agents[openCodeReadOnlyAgent] = agent
	config["agent"] = agents

	merged, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", openCodeConfigContentEnv, err)
	}
	return replaceEnvironment(environ, openCodeConfigContentEnv, string(merged)), nil
}

func objectField(parent map[string]any, key string) (map[string]any, error) {
	value, ok := parent[key]
	if !ok {
		return make(map[string]any), nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return object, nil
}

func openCodePermissionField(agent map[string]any) (map[string]any, error) {
	value, ok := agent["permission"]
	if !ok {
		return make(map[string]any), nil
	}
	if permissions, ok := value.(map[string]any); ok {
		return permissions, nil
	}
	action, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("permission must be an object or a scalar action")
	}
	switch action {
	case "allow", "ask", "deny":
		return map[string]any{"*": action}, nil
	default:
		return nil, fmt.Errorf("permission action %q is invalid", action)
	}
}

func lookupEnvironment(environ []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range environ {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

func replaceEnvironment(environ []string, key, value string) []string {
	prefix := key + "="
	replacement := prefix + value
	result := make([]string, 0, len(environ)+1)
	replaced := false
	for _, entry := range environ {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				result = append(result, replacement)
				replaced = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !replaced {
		result = append(result, replacement)
	}
	return result
}

func verifyOpenCodeReadOnlyPolicy(ctx context.Context, dir string, env []string) error {
	cmd := exec.CommandContext(ctx, "opencode", "debug", "agent", openCodeReadOnlyAgent)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return diagnosticError("opencode read-only policy preflight", err, stdout.Bytes(), stderr.Bytes())
	}
	if err := parseOpenCodeReadOnlyPolicy(stdout.Bytes()); err != nil {
		return diagnosticError("opencode read-only policy preflight", fmt.Errorf("effective policy: %w", err), stdout.Bytes(), stderr.Bytes())
	}
	return nil
}

func parseOpenCodeReadOnlyPolicy(out []byte) error {
	var policy struct {
		Name       string `json:"name"`
		Mode       string `json:"mode"`
		Permission []struct {
			Permission string `json:"permission"`
			Action     string `json:"action"`
			Pattern    string `json:"pattern"`
		} `json:"permission"`
		Tools map[string]bool `json:"tools"`
	}
	if err := json.Unmarshal(out, &policy); err != nil {
		return fmt.Errorf("parsing resolved agent: %w", err)
	}
	if policy.Name != openCodeReadOnlyAgent {
		return fmt.Errorf("resolved agent is %q, want %q", policy.Name, openCodeReadOnlyAgent)
	}
	if policy.Mode != "primary" {
		return fmt.Errorf("resolved agent mode is %q, want primary", policy.Mode)
	}
	for _, permission := range []string{"edit", "bash"} {
		if !effectiveOpenCodeDenial(policy.Permission, permission) {
			return fmt.Errorf("resolved %s permission is not denied", permission)
		}
		if enabled, reported := policy.Tools[permission]; reported && enabled {
			return fmt.Errorf("resolved tools report %s enabled", permission)
		}
	}
	return nil
}

func effectiveOpenCodeDenial(rules []struct {
	Permission string `json:"permission"`
	Action     string `json:"action"`
	Pattern    string `json:"pattern"`
}, permission string) bool {
	lastBlanket := -1
	lastAction := ""
	for i, rule := range rules {
		if rule.Permission != permission && rule.Permission != "*" {
			continue
		}
		if rule.Pattern == "" || rule.Pattern == "*" {
			lastBlanket = i
			lastAction = rule.Action
		}
	}
	if lastAction != "deny" {
		return false
	}
	for _, rule := range rules[lastBlanket+1:] {
		if rule.Permission != permission && rule.Permission != "*" {
			continue
		}
		if rule.Action != "deny" {
			return false
		}
	}
	return true
}
