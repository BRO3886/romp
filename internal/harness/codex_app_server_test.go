package harness

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type scriptedCodexRPC struct {
	messages []rpcMessage
	sent     []rpcMessage
}

func (s *scriptedCodexRPC) Send(message rpcMessage) error {
	s.sent = append(s.sent, message)
	return nil
}

func (s *scriptedCodexRPC) Receive() (rpcMessage, error) {
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message, nil
}

func TestCodexServerArgs(t *testing.T) {
	got := codexServerArgs([]string{"--strict-config"})
	want := []string{"app-server", "--listen", "stdio://", "--strict-config"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codexServerArgs() = %v, want %v", got, want)
	}
}

func TestCodexServerArgsRejectTransportOverrides(t *testing.T) {
	for _, args := range [][]string{{"--listen", "ws://127.0.0.1:4500"}, {"--listen=off"}, {"--stdio"}, {"--code-mode-host", "wss://example.test"}} {
		if err := validateCodexServerArgs(args); err == nil {
			t.Fatalf("validateCodexServerArgs(%v) = nil, want conflict", args)
		}
	}
}

func TestRequestedSkills(t *testing.T) {
	got := requestedSkills("Use $siddhartha-go, then $tdd. Mention $tdd once.")
	want := []string{"siddhartha-go", "tdd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requestedSkills() = %v, want %v", got, want)
	}
}

func TestRunCodexProtocolInjectsSkillsAndReturnsFinalAnswer(t *testing.T) {
	rpc := &scriptedCodexRPC{messages: []rpcMessage{
		response(1, `{"userAgent":"codex"}`),
		response(2, `{"data":[{"cwd":"/tmp/worktree","skills":[{"name":"siddhartha-go","path":"/skills/siddhartha-go/SKILL.md","enabled":true},{"name":"siddhartha-go","path":"/other/siddhartha-go/SKILL.md","enabled":true}],"errors":[]}]}`),
		response(3, `{"thread":{"id":"thread-1","sessionId":"session-1"},"instructionSources":["/tmp/worktree/AGENTS.md"]}`),
		notification("item/completed", `{"threadId":"thread-1","turnId":"turn-1","item":{"type":"agentMessage","text":"working","phase":"commentary"}}`),
		notification("item/completed", `{"threadId":"thread-1","turnId":"turn-1","item":{"type":"agentMessage","text":"done","phase":"final_answer"}}`),
		notification("turn/completed", `{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","error":null}}`),
		response(4, `{"turn":{"id":"turn-1","status":"inProgress"}}`),
	}}

	result, err := runCodexProtocol(context.Background(), rpc, Request{
		Dir:    "/tmp/worktree",
		Prompt: "Use $siddhartha-go to fix it.",
		Model:  "gpt-5.6-terra",
		Effort: "high",
	}, false)
	if err != nil {
		t.Fatalf("runCodexProtocol(): %v", err)
	}
	if result.Output != "done" || result.SessionID != "session-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Metadata == nil || !reflect.DeepEqual(result.Metadata.InstructionSources, []string{"/tmp/worktree/AGENTS.md"}) {
		t.Fatalf("metadata = %+v", result.Metadata)
	}

	turn := sentByMethod(t, rpc.sent, "turn/start")
	var params struct {
		Input []struct {
			Type string `json:"type"`
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"input"`
		ApprovalPolicy string         `json:"approvalPolicy"`
		SandboxPolicy  map[string]any `json:"sandboxPolicy"`
		Model          string         `json:"model"`
		Effort         string         `json:"effort"`
	}
	if err := json.Unmarshal(turn.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Input) != 2 || params.Input[1].Type != "skill" || params.Input[1].Name != "siddhartha-go" || params.Input[1].Path != "/skills/siddhartha-go/SKILL.md" {
		t.Fatalf("turn input = %+v, want text plus resolved skill", params.Input)
	}
	if params.ApprovalPolicy != "never" || params.SandboxPolicy["type"] != "workspaceWrite" || params.Model != "gpt-5.6-terra" || params.Effort != "high" {
		t.Fatalf("turn params = %+v", params)
	}
	thread := sentByMethod(t, rpc.sent, "thread/start")
	var threadParams struct {
		Sandbox string `json:"sandbox"`
	}
	if err := json.Unmarshal(thread.Params, &threadParams); err != nil {
		t.Fatal(err)
	}
	if threadParams.Sandbox != "workspace-write" {
		t.Fatalf("thread sandbox = %q, want workspace-write", threadParams.Sandbox)
	}
}

func TestRunCodexProtocolRejectsUnexpectedResponseID(t *testing.T) {
	rpc := &scriptedCodexRPC{messages: []rpcMessage{response(9, `{}`)}}
	result, err := runCodexProtocol(context.Background(), rpc, Request{Dir: "/tmp/worktree", Prompt: "fix it"}, false)
	if err == nil || !strings.Contains(err.Error(), "unexpected response id 9") {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestRunCodexProtocolFailsWhenRequestedSkillIsMissing(t *testing.T) {
	rpc := &scriptedCodexRPC{messages: []rpcMessage{
		response(1, `{}`),
		response(2, `{"data":[{"cwd":"/tmp/worktree","skills":[],"errors":[]}]}`),
	}}

	result, err := runCodexProtocol(context.Background(), rpc, Request{Dir: "/tmp/worktree", Prompt: "Use the required skill.", Skills: []string{"missing-skill"}}, false)
	if err == nil || !strings.Contains(err.Error(), `requested skill "missing-skill"`) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if sentMethod(rpc.sent, "thread/start") {
		t.Fatal("thread started after requested skill resolution failed")
	}
}

func TestRunCodexProtocolIgnoresDollarWordsThatAreNotInstalledSkills(t *testing.T) {
	rpc := &scriptedCodexRPC{messages: []rpcMessage{
		response(1, `{}`),
		response(2, `{"data":[{"cwd":"/tmp/worktree","skills":[],"errors":[]}]}`),
		response(3, `{"thread":{"id":"thread-1","sessionId":"session-1"},"instructionSources":[]}`),
		response(4, `{"turn":{"id":"turn-1","status":"inProgress"}}`),
		notification("item/completed", `{"threadId":"thread-1","turnId":"turn-1","item":{"type":"agentMessage","text":"done","phase":"final_answer"}}`),
		notification("turn/completed", `{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`),
	}}

	result, err := runCodexProtocol(context.Background(), rpc, Request{Dir: "/tmp/worktree", Prompt: "Do not overwrite $HOME."}, false)
	if err != nil {
		t.Fatalf("runCodexProtocol(): %v", err)
	}
	if result.Metadata == nil || len(result.Metadata.Skills) != 0 {
		t.Fatalf("metadata = %+v, want no injected skills", result.Metadata)
	}
}

func TestRunCodexProtocolRejectsFailedAndInterruptedTurns(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		error   string
		wantErr string
	}{
		{name: "failed", status: "failed", error: `,"error":{"message":"model unavailable"}`, wantErr: "model unavailable"},
		{name: "interrupted", status: "interrupted", wantErr: "interrupted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &scriptedCodexRPC{messages: []rpcMessage{
				response(1, `{}`),
				response(2, `{"thread":{"id":"thread-1","sessionId":"session-1"},"instructionSources":[]}`),
				response(3, `{"turn":{"id":"turn-1","status":"inProgress"}}`),
				notification("turn/completed", `{"threadId":"thread-1","turn":{"id":"turn-1","status":"`+tt.status+`"`+tt.error+`}}`),
			}}
			result, err := runCodexProtocol(context.Background(), rpc, Request{Dir: "/tmp/worktree", Prompt: "fix it"}, false)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

func response(id int, result string) rpcMessage {
	return rpcMessage{ID: json.RawMessage(string(rune('0' + id))), Result: json.RawMessage(result)}
}

func notification(method, params string) rpcMessage {
	return rpcMessage{Method: method, Params: json.RawMessage(params)}
}

func sentByMethod(t *testing.T, messages []rpcMessage, method string) rpcMessage {
	t.Helper()
	for _, message := range messages {
		if message.Method == method {
			return message
		}
	}
	t.Fatalf("no %s message in %+v", method, messages)
	return rpcMessage{}
}

func sentMethod(messages []rpcMessage, method string) bool {
	for _, message := range messages {
		if message.Method == method {
			return true
		}
	}
	return false
}
