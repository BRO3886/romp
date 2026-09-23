package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

var skillMarker = regexp.MustCompile(`\$([A-Za-z0-9][A-Za-z0-9_-]*)`)

// Codex drives Codex through one app-server process per run.
type Codex struct {
	// Args are extra arguments passed to `codex app-server`, such as
	// ["--strict-config"] or ["--config", "features.unified_exec=true"].
	Args []string
	// Ephemeral keeps the created Codex thread out of persisted history.
	Ephemeral bool
}

func (c Codex) Name() string { return "codex" }

// Check verifies that the Codex CLI and app-server command are installed.
func (c Codex) Check(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return "", fmt.Errorf("codex CLI not found on PATH: install Codex and log in")
	}
	ver, err := exec.CommandContext(ctx, "codex", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codex --version failed: %w\n%s", err, ver)
	}
	if out, err := exec.CommandContext(ctx, "codex", "app-server", "--help").CombinedOutput(); err != nil {
		return "", fmt.Errorf("codex app-server is unavailable: %w\n%s", err, out)
	}
	if out, err := exec.CommandContext(ctx, "codex", "doctor").CombinedOutput(); err != nil {
		return "", fmt.Errorf("codex doctor reported a problem: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(ver)), nil
}

func (c Codex) Run(ctx context.Context, req Request) (Result, error) {
	if err := validateCodexServerArgs(c.Args); err != nil {
		return Result{}, err
	}
	if req.ReadOnly {
		if err := validateCodexReadOnlyExtras(c.Args); err != nil {
			return Result{}, err
		}
	}
	process, err := startCodexServer(ctx, req.Dir, codexServerArgs(c.Args))
	if err != nil {
		return Result{}, fmt.Errorf("codex app-server start: %w", err)
	}

	result, runErr := runCodexProtocol(ctx, process, req, c.Ephemeral)
	shutdownErr := process.shutdown(runErr != nil)
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			runErr = ctxErr
		}
		return Result{}, diagnosticError("codex app-server", runErr, nil, process.stderr.Bytes())
	}
	if shutdownErr != nil {
		return Result{}, diagnosticError("codex app-server shutdown", shutdownErr, nil, process.stderr.Bytes())
	}
	return result, nil
}

func codexServerArgs(extra []string) []string {
	args := []string{"app-server", "--listen", "stdio://"}
	return append(args, extra...)
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type codexRPC interface {
	Send(rpcMessage) error
	Receive() (rpcMessage, error)
}

type codexProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	stderr bytes.Buffer
}

func startCodexServer(ctx context.Context, dir string, args []string) (*codexProcess, error) {
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	process := &codexProcess{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout)}
	cmd.Stderr = &process.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return process, nil
}

func (p *codexProcess) Send(message rpcMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func (p *codexProcess) Receive() (rpcMessage, error) {
	line, err := p.reader.ReadBytes('\n')
	if err != nil {
		return rpcMessage{}, err
	}
	var message rpcMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return rpcMessage{}, fmt.Errorf("decode message: %w", err)
	}
	return message, nil
}

func (p *codexProcess) shutdown(force bool) error {
	var shutdownErrs []error
	if force && p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("kill process: %w", err))
		}
	}
	if err := p.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		shutdownErrs = append(shutdownErrs, fmt.Errorf("close stdin: %w", err))
	}
	if err := p.cmd.Wait(); err != nil {
		if !force && !errors.Is(err, context.Canceled) {
			shutdownErrs = append(shutdownErrs, err)
		}
	}
	return errors.Join(shutdownErrs...)
}

type rpcClient struct {
	transport codexRPC
	nextID    int
}

func newRPCClient(transport codexRPC) *rpcClient {
	return &rpcClient{transport: transport, nextID: 1}
}

func (c *rpcClient) notify(method string, params any) error {
	message, err := requestMessage(nil, method, params)
	if err != nil {
		return err
	}
	return c.transport.Send(message)
}

func (c *rpcClient) call(method string, params, result any, handle func(rpcMessage) error) error {
	idJSON := json.RawMessage(fmt.Sprintf("%d", c.nextID))
	c.nextID++
	message, err := requestMessage(idJSON, method, params)
	if err != nil {
		return err
	}
	if err := c.transport.Send(message); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}

	for {
		incoming, err := c.transport.Receive()
		if err != nil {
			return fmt.Errorf("receive %s response: %w", method, err)
		}
		if len(incoming.ID) > 0 && incoming.Method != "" {
			_ = c.transport.Send(rpcMessage{ID: incoming.ID, Error: &rpcError{Code: -32601, Message: "romp does not support interactive server requests"}})
			return fmt.Errorf("%s requested unsupported interaction %q", method, incoming.Method)
		}
		if len(incoming.ID) == 0 {
			if incoming.Method == "" {
				return fmt.Errorf("%s received message without id or method", method)
			}
			if handle != nil {
				if err := handle(incoming); err != nil {
					return err
				}
			}
			continue
		}
		if string(incoming.ID) != string(idJSON) {
			return fmt.Errorf("%s received unexpected response id %s", method, incoming.ID)
		}
		if incoming.Error != nil {
			return fmt.Errorf("%s rejected (%d): %s", method, incoming.Error.Code, incoming.Error.Message)
		}
		if result == nil {
			return nil
		}
		if len(incoming.Result) == 0 {
			return fmt.Errorf("%s returned no result", method)
		}
		if err := json.Unmarshal(incoming.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func requestMessage(id json.RawMessage, method string, params any) (rpcMessage, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return rpcMessage{}, fmt.Errorf("encode %s params: %w", method, err)
	}
	return rpcMessage{ID: id, Method: method, Params: encoded}, nil
}

type codexInput struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	Text string `json:"text,omitempty"`
}

func runCodexProtocol(ctx context.Context, transport codexRPC, req Request, ephemeral bool) (Result, error) {
	client := newRPCClient(transport)
	var initialized struct{}
	if err := client.call("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "romp", "title": "Romp", "version": "0.1"},
	}, &initialized, nil); err != nil {
		return Result{}, fmt.Errorf("initialize: %w", err)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return Result{}, fmt.Errorf("acknowledge initialization: %w", err)
	}

	inputs := []codexInput{{Type: "text", Text: req.Prompt}}
	markers := requestedSkills(req.Prompt)
	loadedSkills := append([]string(nil), req.Skills...)
	if len(req.Skills) > 0 || len(markers) > 0 {
		skills, names, err := listRequestedSkills(client, req.Dir, req.Skills, markers)
		if err != nil {
			return Result{}, err
		}
		inputs = append(inputs, skills...)
		loadedSkills = names
	}

	threadSandbox := "workspace-write"
	turnSandbox := "workspaceWrite"
	if req.ReadOnly {
		threadSandbox = "read-only"
		turnSandbox = "readOnly"
	}
	threadParams := map[string]any{
		"approvalPolicy": "never",
		"cwd":            req.Dir,
		"ephemeral":      ephemeral,
		"sandbox":        threadSandbox,
		"serviceName":    "romp",
	}
	if req.Model != "" {
		threadParams["model"] = req.Model
	}
	var threadResponse struct {
		Thread struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		} `json:"thread"`
		InstructionSources []string `json:"instructionSources"`
	}
	if err := client.call("thread/start", threadParams, &threadResponse, nil); err != nil {
		return Result{}, fmt.Errorf("start thread: %w", err)
	}
	if threadResponse.Thread.ID == "" || threadResponse.Thread.SessionID == "" {
		return Result{}, fmt.Errorf("start thread: response has no thread or session id")
	}

	turnParams := map[string]any{
		"approvalPolicy": "never",
		"cwd":            req.Dir,
		"input":          inputs,
		"sandboxPolicy":  map[string]string{"type": turnSandbox},
		"threadId":       threadResponse.Thread.ID,
	}
	if req.Model != "" {
		turnParams["model"] = req.Model
	}
	if req.Effort != "" {
		turnParams["effort"] = req.Effort
	}
	var turnResponse struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	state := turnState{threadID: threadResponse.Thread.ID}
	var earlyEvents []rpcMessage
	if err := client.call("turn/start", turnParams, &turnResponse, func(message rpcMessage) error {
		earlyEvents = append(earlyEvents, message)
		return nil
	}); err != nil {
		return Result{}, fmt.Errorf("start turn: %w", err)
	}
	if turnResponse.Turn.ID == "" {
		return Result{}, fmt.Errorf("start turn: response has no turn id")
	}
	state.turnID = turnResponse.Turn.ID
	for _, message := range earlyEvents {
		done, err := state.consume(message)
		if err != nil {
			return Result{}, err
		}
		if done {
			return codexResult(state, threadResponse.Thread.SessionID, threadResponse.InstructionSources, loadedSkills)
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		message, err := transport.Receive()
		if err != nil {
			return Result{}, fmt.Errorf("receive turn event: %w", err)
		}
		if len(message.ID) > 0 && message.Method != "" {
			_ = transport.Send(rpcMessage{ID: message.ID, Error: &rpcError{Code: -32601, Message: "romp does not support interactive server requests"}})
			return Result{}, fmt.Errorf("turn requested unsupported interaction %q", message.Method)
		}
		if len(message.ID) > 0 {
			return Result{}, fmt.Errorf("turn received unexpected response id %s", message.ID)
		}
		if message.Method == "" {
			return Result{}, fmt.Errorf("turn received message without id or method")
		}
		done, err := state.consume(message)
		if err != nil {
			return Result{}, err
		}
		if !done {
			continue
		}
		return codexResult(state, threadResponse.Thread.SessionID, threadResponse.InstructionSources, loadedSkills)
	}
}

func codexResult(state turnState, sessionID string, instructionSources, skills []string) (Result, error) {
	output := state.finalOutput()
	if strings.TrimSpace(output) == "" {
		return Result{}, fmt.Errorf("completed turn has no non-empty agent message")
	}
	return Result{
		Output:    output,
		SessionID: sessionID,
		Metadata: &ResultMetadata{
			InstructionSources: instructionSources,
			Skills:             skills,
		},
	}, nil
}

func requestedSkills(prompt string) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, match := range skillMarker.FindAllStringSubmatch(prompt, -1) {
		if _, exists := seen[match[1]]; exists {
			continue
		}
		seen[match[1]] = struct{}{}
		names = append(names, match[1])
	}
	return names
}

func listRequestedSkills(client *rpcClient, cwd string, required, markers []string) ([]codexInput, []string, error) {
	var response struct {
		Data []struct {
			CWD    string `json:"cwd"`
			Skills []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Enabled bool   `json:"enabled"`
			} `json:"skills"`
			Errors []any `json:"errors"`
		} `json:"data"`
	}
	if err := client.call("skills/list", map[string]any{"cwds": []string{cwd}, "forceReload": true}, &response, nil); err != nil {
		return nil, nil, fmt.Errorf("list requested skills: %w", err)
	}
	if len(response.Data) != 1 || response.Data[0].CWD != cwd {
		return nil, nil, fmt.Errorf("list requested skills: response does not contain cwd %q", cwd)
	}
	available := make(map[string][]codexInput)
	for _, skill := range response.Data[0].Skills {
		if !skill.Enabled {
			continue
		}
		available[skill.Name] = append(available[skill.Name], codexInput{Type: "skill", Name: skill.Name, Path: skill.Path})
	}
	requested := append(append([]string(nil), required...), markers...)
	requested = uniqueStrings(requested)
	resolved := make([]codexInput, 0, len(requested))
	var names []string
	for _, name := range requested {
		matches := available[name]
		if len(matches) == 0 {
			if slices.Contains(required, name) {
				return nil, nil, fmt.Errorf("requested skill %q is not installed and enabled for %q", name, cwd)
			}
			continue
		}
		resolved = append(resolved, matches[0])
		names = append(names, name)
	}
	return resolved, names, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

type turnState struct {
	threadID      string
	turnID        string
	lastMessage   string
	finalMessages []string
}

func (s *turnState) consume(message rpcMessage) (bool, error) {
	switch message.Method {
	case "error":
		var params struct {
			ThreadID string `json:"threadId"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, fmt.Errorf("decode error event: %w", err)
		}
		if params.ThreadID == "" || params.ThreadID == s.threadID {
			return false, fmt.Errorf("turn failed: %s", emptyFallback(params.Error.Message, "unknown error"))
		}
	case "item/completed":
		var params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Phase string `json:"phase"`
			} `json:"item"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, fmt.Errorf("decode item/completed: %w", err)
		}
		if params.ThreadID != s.threadID || (s.turnID != "" && params.TurnID != s.turnID) || params.Item.Type != "agentMessage" {
			return false, nil
		}
		s.lastMessage = params.Item.Text
		if params.Item.Phase == "final_answer" {
			s.finalMessages = append(s.finalMessages, params.Item.Text)
		}
	case "turn/completed":
		var params struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return false, fmt.Errorf("decode turn/completed: %w", err)
		}
		if params.ThreadID != s.threadID || params.Turn.ID != s.turnID {
			return false, nil
		}
		switch params.Turn.Status {
		case "completed":
			return true, nil
		case "failed":
			failure := "unknown failure"
			if params.Turn.Error != nil {
				failure = emptyFallback(params.Turn.Error.Message, failure)
			}
			return false, fmt.Errorf("turn failed: %s", failure)
		case "interrupted":
			return false, fmt.Errorf("turn interrupted")
		default:
			return false, fmt.Errorf("turn completed with unexpected status %q", params.Turn.Status)
		}
	}
	return false, nil
}

func (s *turnState) finalOutput() string {
	if len(s.finalMessages) > 0 {
		return strings.Join(s.finalMessages, "\n")
	}
	return s.lastMessage
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func validateCodexReadOnlyExtras(extra []string) error {
	for _, override := range codexConfigOverrides(extra) {
		key, _, _ := strings.Cut(override, "=")
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		if key == "approval_policy" || key == "sandbox_mode" || strings.HasPrefix(key, "sandbox_") {
			return fmt.Errorf("codex read-only run rejects conflicting config override %q", key)
		}
	}
	return nil
}

func validateCodexServerArgs(extra []string) error {
	flag, conflict := conflictingExtra(extra, "--listen", "--stdio", "--code-mode-host")
	if conflict {
		return fmt.Errorf("codex app-server rejects conflicting extra argument %q", flag)
	}
	return nil
}

func codexConfigOverrides(args []string) []string {
	var overrides []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-c" || arg == "--config":
			if i+1 < len(args) {
				overrides = append(overrides, args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--config="):
			overrides = append(overrides, strings.TrimPrefix(arg, "--config="))
		case strings.HasPrefix(arg, "-c="):
			overrides = append(overrides, strings.TrimPrefix(arg, "-c="))
		case strings.HasPrefix(arg, "-c") && !strings.HasPrefix(arg, "--"):
			overrides = append(overrides, strings.TrimPrefix(arg, "-c"))
		}
	}
	return overrides
}
