package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyCommandConstruction(t *testing.T) {
	tests := []struct {
		name  string
		build func(Request, []string) []string
		req   Request
		extra []string
		want  []string
	}{
		{
			name:  "claude writable",
			build: claudeArgs,
			req:   Request{Prompt: "review", Model: "sonnet", Effort: "high", MaxTurns: 12},
			extra: []string{"--verbose"},
			want:  []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions", "--model", "sonnet", "--effort", "high", "--max-turns", "12", "--verbose", "review"},
		},
		{
			name:  "claude read only",
			build: claudeArgs,
			req:   Request{Prompt: "review", Model: "sonnet", Effort: "high", MaxTurns: 12, ReadOnly: true},
			extra: []string{"--verbose"},
			want:  []string{"-p", "--output-format", "json", "--model", "sonnet", "--effort", "high", "--max-turns", "12", "--verbose", "--permission-mode", "bypassPermissions", "--disallowedTools=Write,Edit,NotebookEdit,Bash", "review"},
		},
		{
			name:  "codex writable",
			build: codexArgs,
			req:   Request{Prompt: "review", Dir: "/tmp/wt", Model: "gpt-5.6-terra", Effort: "high"},
			extra: []string{"--ephemeral"},
			want:  []string{"exec", "--json", "--sandbox", "workspace-write", "--color", "never", "--cd", "/tmp/wt", "--model", "gpt-5.6-terra", "-c", "model_reasoning_effort=high", "--ephemeral"},
		},
		{
			name:  "codex read only",
			build: codexArgs,
			req:   Request{Prompt: "review", Dir: "/tmp/wt", Model: "gpt-5.6-terra", Effort: "high", ReadOnly: true},
			extra: []string{"--ephemeral"},
			want:  []string{"exec", "--json", "--color", "never", "--cd", "/tmp/wt", "--model", "gpt-5.6-terra", "-c", "model_reasoning_effort=high", "--ephemeral", "--sandbox", "read-only"},
		},
		{
			name:  "opencode writable",
			build: opencodeArgs,
			req:   Request{Prompt: "review", Model: "openai/gpt-5", Effort: "high"},
			extra: []string{"--title", "review run"},
			want:  []string{"run", "--auto", "--format", "json", "--model", "openai/gpt-5", "--variant", "high", "--title", "review run", "review"},
		},
		{
			name:  "opencode read only",
			build: opencodeArgs,
			req:   Request{Prompt: "review", Model: "openai/gpt-5", Effort: "high", ReadOnly: true},
			extra: []string{"--title", "review run"},
			want:  []string{"run", "--auto", "--format", "json", "--model", "openai/gpt-5", "--variant", "high", "--title", "review run", "--agent", "romp-read-only", "review"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build(tt.req, tt.extra); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("arguments = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadOnlyConflictingExtras(t *testing.T) {
	tests := []struct {
		name     string
		validate func([]string) error
		extra    []string
		want     string
	}{
		{name: "claude permission mode", validate: validateClaudeReadOnlyExtras, extra: []string{"--permission-mode", "acceptEdits"}, want: "--permission-mode"},
		{name: "claude permission mode equals", validate: validateClaudeReadOnlyExtras, extra: []string{"--permission-mode=acceptEdits"}, want: "--permission-mode"},
		{name: "claude allowed tools", validate: validateClaudeReadOnlyExtras, extra: []string{"--allowedTools", "Read,Bash"}, want: "--allowedTools"},
		{name: "claude allowed tools alias", validate: validateClaudeReadOnlyExtras, extra: []string{"--allowed-tools=Read,Bash"}, want: "--allowed-tools"},
		{name: "claude tools", validate: validateClaudeReadOnlyExtras, extra: []string{"--tools", "Read,Bash"}, want: "--tools"},
		{name: "claude tools equals", validate: validateClaudeReadOnlyExtras, extra: []string{"--tools=Read,Bash"}, want: "--tools"},
		{name: "claude disallowed tools", validate: validateClaudeReadOnlyExtras, extra: []string{"--disallowedTools", "Write"}, want: "--disallowedTools"},
		{name: "claude disallowed tools alias", validate: validateClaudeReadOnlyExtras, extra: []string{"--disallowed-tools=Write"}, want: "--disallowed-tools"},
		{name: "claude allow permission bypass", validate: validateClaudeReadOnlyExtras, extra: []string{"--allow-dangerously-skip-permissions"}, want: "--allow-dangerously-skip-permissions"},
		{name: "claude allow permission bypass equals", validate: validateClaudeReadOnlyExtras, extra: []string{"--allow-dangerously-skip-permissions=true"}, want: "--allow-dangerously-skip-permissions"},
		{name: "claude permission bypass", validate: validateClaudeReadOnlyExtras, extra: []string{"--dangerously-skip-permissions"}, want: "--dangerously-skip-permissions"},
		{name: "claude option terminator", validate: validateClaudeReadOnlyExtras, extra: []string{"--"}, want: "--"},
		{name: "codex sandbox", validate: validateCodexReadOnlyExtras, extra: []string{"--sandbox", "workspace-write"}, want: "--sandbox"},
		{name: "codex sandbox equals", validate: validateCodexReadOnlyExtras, extra: []string{"--sandbox=danger-full-access"}, want: "--sandbox"},
		{name: "codex sandbox short", validate: validateCodexReadOnlyExtras, extra: []string{"-s", "workspace-write"}, want: "-s"},
		{name: "codex sandbox attached short", validate: validateCodexReadOnlyExtras, extra: []string{"-sworkspace-write"}, want: "-s"},
		{name: "codex writable directory", validate: validateCodexReadOnlyExtras, extra: []string{"--add-dir", "/tmp"}, want: "--add-dir"},
		{name: "codex writable directory equals", validate: validateCodexReadOnlyExtras, extra: []string{"--add-dir=/tmp"}, want: "--add-dir"},
		{name: "codex sandbox bypass", validate: validateCodexReadOnlyExtras, extra: []string{"--dangerously-bypass-approvals-and-sandbox"}, want: "--dangerously-bypass-approvals-and-sandbox"},
		{name: "codex workspace approval", validate: validateCodexReadOnlyExtras, extra: []string{"--approve-for-me"}, want: "--approve-for-me"},
		{name: "codex output file", validate: validateCodexReadOnlyExtras, extra: []string{"--output-last-message", "review.md"}, want: "--output-last-message"},
		{name: "codex output file short", validate: validateCodexReadOnlyExtras, extra: []string{"-o", "review.md"}, want: "-o"},
		{name: "codex output file attached short", validate: validateCodexReadOnlyExtras, extra: []string{"-oreview.md"}, want: "-o"},
		{name: "codex sandbox config", validate: validateCodexReadOnlyExtras, extra: []string{"-c", `sandbox_mode="workspace-write"`}, want: "sandbox_mode"},
		{name: "codex sandbox config whitespace", validate: validateCodexReadOnlyExtras, extra: []string{"-c", ` sandbox_mode = "workspace-write"`}, want: "sandbox_mode"},
		{name: "codex quoted sandbox config", validate: validateCodexReadOnlyExtras, extra: []string{"-c", `"sandbox_mode"="workspace-write"`}, want: "sandbox_mode"},
		{name: "codex sandbox config attached", validate: validateCodexReadOnlyExtras, extra: []string{`-csandbox_permissions=["disk-full-write-access"]`}, want: "sandbox_permissions"},
		{name: "codex approval config equals", validate: validateCodexReadOnlyExtras, extra: []string{`--config=approval_policy="never"`}, want: "approval_policy"},
		{name: "codex option terminator", validate: validateCodexReadOnlyExtras, extra: []string{"--"}, want: "--"},
		{name: "opencode agent", validate: validateOpenCodeReadOnlyExtras, extra: []string{"--agent", "build"}, want: "--agent"},
		{name: "opencode agent equals", validate: validateOpenCodeReadOnlyExtras, extra: []string{"--agent=build"}, want: "--agent"},
		{name: "opencode remote server", validate: validateOpenCodeReadOnlyExtras, extra: []string{"--attach", "http://localhost:4096"}, want: "--attach"},
		{name: "opencode option terminator", validate: validateOpenCodeReadOnlyExtras, extra: []string{"--"}, want: "--"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validate(tt.extra)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validation error = %v, want conflict containing %q", err, tt.want)
			}
		})
	}
}

func TestReadOnlyConflictFailsBeforeProcessExecution(t *testing.T) {
	tests := []struct {
		name   string
		binary string
		run    func(context.Context, Request) (Result, error)
	}{
		{name: "claude permission mode", binary: "claude", run: (Claude{Args: []string{"--permission-mode", "acceptEdits"}}).Run},
		{name: "claude tools", binary: "claude", run: (Claude{Args: []string{"--tools", "Read,Bash"}}).Run},
		{name: "codex", binary: "codex", run: (Codex{Args: []string{"--sandbox", "workspace-write"}}).Run},
		{name: "opencode", binary: "opencode", run: (OpenCode{Args: []string{"--agent", "build"}}).Run},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			started := filepath.Join(t.TempDir(), "started")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("STARTED", started)
			writeHarnessScript(t, bin, tt.binary, "printf started > \"$STARTED\"\nexit 99\n")

			result, err := tt.run(context.Background(), Request{Dir: t.TempDir(), Prompt: "review", ReadOnly: true})
			if err == nil || !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("Run result = %+v, error = %v; want read-only conflict", result, err)
			}
			if _, err := os.Stat(started); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("child process started before conflict rejection: %v", err)
			}
		})
	}
}

func TestOpenCodeEnvironmentConstruction(t *testing.T) {
	base := []string{"A=1", `OPENCODE_CONFIG_CONTENT={"theme":"dark","agent":{"other":{"mode":"primary"},"romp-read-only":{"model":"openai/gpt-5","permission":{"webfetch":"ask","edit":"allow"}}}}`, "B=2"}

	writable, err := opencodeEnv(false, base)
	if err != nil {
		t.Fatalf("writable environment: %v", err)
	}
	if !reflect.DeepEqual(writable, base) {
		t.Errorf("writable environment = %v, want byte-for-byte input %v", writable, base)
	}

	readOnly, err := opencodeEnv(true, base)
	if err != nil {
		t.Fatalf("read-only environment: %v", err)
	}
	if got := envValue(readOnly, "A"); got != "1" {
		t.Errorf("A = %q, want preserved value", got)
	}
	if got := envValue(readOnly, "B"); got != "2" {
		t.Errorf("B = %q, want preserved value", got)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(envValue(readOnly, openCodeConfigContentEnv)), &config); err != nil {
		t.Fatalf("merged OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if config["theme"] != "dark" {
		t.Errorf("theme = %#v, want preserved dark", config["theme"])
	}
	agents := config["agent"].(map[string]any)
	if _, ok := agents["other"]; !ok {
		t.Error("unrelated agent was not preserved")
	}
	readOnlyAgent := agents[openCodeReadOnlyAgent].(map[string]any)
	if readOnlyAgent["mode"] != "primary" || readOnlyAgent["model"] != "openai/gpt-5" {
		t.Errorf("read-only agent = %#v, want enforced mode and preserved model", readOnlyAgent)
	}
	permissions := readOnlyAgent["permission"].(map[string]any)
	for key, want := range map[string]any{"edit": "deny", "bash": "deny", "webfetch": "ask"} {
		if permissions[key] != want {
			t.Errorf("permission %s = %#v, want %#v", key, permissions[key], want)
		}
	}
}

func TestOpenCodeEnvironmentConvertsScalarPermission(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		t.Run(action, func(t *testing.T) {
			base := []string{openCodeConfigContentEnv + `={"agent":{"romp-read-only":{"permission":"` + action + `"}}}`}
			readOnly, err := opencodeEnv(true, base)
			if err != nil {
				t.Fatalf("read-only environment: %v", err)
			}

			var config map[string]any
			if err := json.Unmarshal([]byte(envValue(readOnly, openCodeConfigContentEnv)), &config); err != nil {
				t.Fatalf("merged OPENCODE_CONFIG_CONTENT: %v", err)
			}
			agent := config["agent"].(map[string]any)[openCodeReadOnlyAgent].(map[string]any)
			permissions := agent["permission"].(map[string]any)
			for key, want := range map[string]any{"*": action, "edit": "deny", "bash": "deny"} {
				if permissions[key] != want {
					t.Errorf("permission %s = %#v, want %#v", key, permissions[key], want)
				}
			}
		})
	}
}

func TestOpenCodeEnvironmentRejectsInvalidExistingConfig(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantDetail string
	}{
		{name: "invalid JSON", value: "{"},
		{name: "null JSON", value: "null"},
		{name: "non-object JSON", value: "[]"},
		{name: "non-object agent", value: `{"agent":[]}`},
		{name: "non-object target agent", value: `{"agent":{"romp-read-only":[]}}`},
		{name: "non-object permissions", value: `{"agent":{"romp-read-only":{"permission":[]}}}`},
		{name: "unknown scalar permission", value: `{"agent":{"romp-read-only":{"permission":"sometimes"}}}`, wantDetail: `permission action "sometimes" is invalid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := opencodeEnv(true, []string{openCodeConfigContentEnv + "=" + tt.value})
			if err == nil || !strings.Contains(err.Error(), openCodeConfigContentEnv) {
				t.Fatalf("opencodeEnv error = %v, want contextual config error", err)
			}
			if tt.wantDetail != "" && !strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("opencodeEnv error = %v, want detail %q", err, tt.wantDetail)
			}
		})
	}
}

func TestOpenCodeInvalidConfigFailsBeforeProcessExecution(t *testing.T) {
	bin := t.TempDir()
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STARTED", started)
	t.Setenv(openCodeConfigContentEnv, "{")
	writeHarnessScript(t, bin, "opencode", "printf started > \"$STARTED\"\nexit 99\n")

	result, err := (OpenCode{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "review", ReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), openCodeConfigContentEnv) {
		t.Fatalf("Run result = %+v, error = %v; want invalid config rejection", result, err)
	}
	if _, err := os.Stat(started); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child process started before invalid config rejection: %v", err)
	}
}

func TestParseOpenCodeReadOnlyPolicy(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "effective denials",
			payload: `{"name":"romp-read-only","mode":"primary","permission":[{"permission":"*","action":"allow","pattern":"*"},{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}],"tools":{"edit":false,"bash":false}}`,
		},
		{
			name:    "wrong agent",
			payload: `{"name":"build","mode":"primary","permission":[{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}]}`,
			wantErr: "agent",
		},
		{
			name:    "wrong mode",
			payload: `{"name":"romp-read-only","mode":"subagent","permission":[{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}]}`,
			wantErr: "primary",
		},
		{
			name:    "edit allowed",
			payload: `{"name":"romp-read-only","mode":"primary","permission":[{"permission":"edit","action":"allow","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}]}`,
			wantErr: "edit",
		},
		{
			name:    "later bash allow",
			payload: `{"name":"romp-read-only","mode":"primary","permission":[{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"},{"permission":"bash","action":"allow","pattern":"git *"}]}`,
			wantErr: "bash",
		},
		{
			name:    "tools report edit enabled",
			payload: `{"name":"romp-read-only","mode":"primary","permission":[{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}],"tools":{"edit":true,"bash":false}}`,
			wantErr: "edit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseOpenCodeReadOnlyPolicy([]byte(tt.payload))
			if tt.wantErr == "" && err != nil {
				t.Fatalf("parseOpenCodeReadOnlyPolicy: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("parseOpenCodeReadOnlyPolicy error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReadOnlyRunForwardsStructuredOutputWithoutArtifact(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		run        func(context.Context, Request) (Result, error)
		wantArgs   []string
		wantOutput string
		wantID     string
	}{
		{
			name: "claude", fixture: "claude-2.1.235-success.json", run: (Claude{}).Run,
			wantArgs:   []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions", "--disallowedTools=Write,Edit,NotebookEdit,Bash", "rendered prompt"},
			wantOutput: "Claude completed the task.", wantID: "902816de-f8a8-402b-a198-242830f8d818",
		},
		{
			name: "codex", fixture: "codex-0.147.0-success.jsonl", run: (Codex{}).Run,
			wantArgs:   []string{"exec", "--json", "--color", "never", "--cd", "WORKTREE", "--sandbox", "read-only"},
			wantOutput: "Codex completed the task.", wantID: "019d1c0a-0137-73f3-bf4a-88c90739150c",
		},
		{
			name: "opencode", fixture: "opencode-1.18.18-success.jsonl", run: (OpenCode{}).Run,
			wantArgs:   []string{"run", "--auto", "--format", "json", "--agent", "romp-read-only", "rendered prompt"},
			wantOutput: "Applied the requested changes.\nOpenCode completed the task.", wantID: "ses_65b3acf58ffeLSa4dfj1RVoPpW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			captureDir := t.TempDir()
			worktree := t.TempDir()
			fixture, err := filepath.Abs(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FIXTURE", fixture)
			t.Setenv("CAPTURE_ARGS", filepath.Join(captureDir, "args"))
			t.Setenv("CAPTURE_STDIN", filepath.Join(captureDir, "stdin"))
			t.Setenv("CAPTURE_CONFIG", filepath.Join(captureDir, "config"))
			t.Setenv("CAPTURE_PREFLIGHT_CONFIG", filepath.Join(captureDir, "preflight-config"))
			t.Setenv(openCodeConfigContentEnv, `{"theme":"dark","agent":{"romp-read-only":{"permission":"allow"}}}`)
			writeHarnessScript(t, bin, tt.name, `
if [ "$1" = debug ]; then
  [ "$2" = agent ] || exit 91
  [ "$3" = romp-read-only ] || exit 92
  [ "$#" -eq 3 ] || exit 93
  printf '%s' "${OPENCODE_CONFIG_CONTENT-}" > "$CAPTURE_PREFLIGHT_CONFIG"
  printf '%s\n' '{"name":"romp-read-only","mode":"primary","permission":[{"permission":"edit","action":"deny","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}],"tools":{"edit":false,"bash":false}}'
  exit 0
fi
printf '%s\n' "$@" > "$CAPTURE_ARGS"
printf '%s' "${OPENCODE_CONFIG_CONTENT-}" > "$CAPTURE_CONFIG"
if [ "$1" = exec ] || [ "$1" = -p ]; then cat > "$CAPTURE_STDIN"; fi
cat "$FIXTURE"
`)

			result, err := tt.run(context.Background(), Request{Dir: worktree, Prompt: "rendered prompt", ReadOnly: true})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.Output != tt.wantOutput || result.SessionID != tt.wantID {
				t.Errorf("result = %+v, want output %q and session %q", result, tt.wantOutput, tt.wantID)
			}
			captured, err := os.ReadFile(filepath.Join(captureDir, "args"))
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := append([]string(nil), tt.wantArgs...)
			for i := range wantArgs {
				if wantArgs[i] == "WORKTREE" {
					wantArgs[i] = worktree
				}
			}
			gotArgs := strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("child arguments = %v, want %v", gotArgs, wantArgs)
			}
			if tt.name == "claude" || tt.name == "codex" {
				stdin, err := os.ReadFile(filepath.Join(captureDir, "stdin"))
				if err != nil {
					t.Fatal(err)
				}
				wantStdin := ""
				if tt.name == "codex" {
					wantStdin = "rendered prompt"
				}
				if string(stdin) != wantStdin {
					t.Errorf("stdin = %q, want %q", stdin, wantStdin)
				}
			}
			configBytes, err := os.ReadFile(filepath.Join(captureDir, "config"))
			if err != nil {
				t.Fatal(err)
			}
			config := string(configBytes)
			if tt.name == "opencode" {
				preflightConfig, err := os.ReadFile(filepath.Join(captureDir, "preflight-config"))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(preflightConfig, configBytes) {
					t.Errorf("preflight and run environments differ")
				}
				var merged map[string]any
				if err := json.Unmarshal(configBytes, &merged); err != nil {
					t.Fatalf("child OPENCODE_CONFIG_CONTENT: %v", err)
				}
				if merged["theme"] != "dark" {
					t.Errorf("child config theme = %#v, want preserved dark", merged["theme"])
				}
				agent := merged["agent"].(map[string]any)[openCodeReadOnlyAgent].(map[string]any)
				permissions := agent["permission"].(map[string]any)
				if agent["mode"] != "primary" || permissions["*"] != "allow" || permissions["edit"] != "deny" || permissions["bash"] != "deny" {
					t.Errorf("child read-only agent config = %#v", agent)
				}
			} else if config != `{"theme":"dark","agent":{"romp-read-only":{"permission":"allow"}}}` {
				t.Errorf("child config = %q, want unchanged inherited value", config)
			}
			entries, err := os.ReadDir(worktree)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("read-only run created worktree artifacts: %v", entries)
			}
		})
	}
}

func TestOpenCodePreflightRejectsWeakenedPolicyBeforeRun(t *testing.T) {
	bin := t.TempDir()
	runStarted := filepath.Join(t.TempDir(), "run-started")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RUN_STARTED", runStarted)
	writeHarnessScript(t, bin, "opencode", `
if [ "$1" = debug ]; then
  printf '%s\n' '{"name":"romp-read-only","mode":"primary","permission":[{"permission":"edit","action":"allow","pattern":"*"},{"permission":"bash","action":"deny","pattern":"*"}]}'
  exit 0
fi
printf started > "$RUN_STARTED"
exit 99
`)

	result, err := (OpenCode{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "review", ReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), "effective") {
		t.Fatalf("Run result = %+v, error = %v; want effective-policy rejection", result, err)
	}
	if _, err := os.Stat(runStarted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenCode run started after failed preflight: %v", err)
	}
}

func TestLiveReadOnlyEnforcement(t *testing.T) {
	if os.Getenv("ROMP_LIVE_HARNESS_TESTS") != "1" {
		t.Skip("set ROMP_LIVE_HARNESS_TESTS=1 to run authenticated CLI enforcement tests")
	}

	tests := []struct {
		name string
		run  func(context.Context, Request) (Result, error)
	}{
		{name: "claude", run: (Claude{Args: []string{"--no-session-persistence"}}).Run},
		{name: "codex", run: (Codex{Args: []string{"--ephemeral"}}).Run},
		{name: "opencode", run: (OpenCode{}).Run},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := exec.LookPath(tt.name); err != nil {
				t.Skipf("%s CLI is not installed: %v", tt.name, err)
			}
			worktree := t.TempDir()
			if out, err := exec.Command("git", "-C", worktree, "init", "--quiet").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
			sentinel := filepath.Join(worktree, "sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("original\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			prompt := "Attempt both mutations even after a denial. First use the native edit or write tool to replace sentinel.txt with edited. Then use the shell to replace sentinel.txt with shell. Do not use another mutation path. Finish with a brief report of the native denial evidence."
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			result, err := tt.run(ctx, Request{Dir: worktree, Prompt: prompt, ReadOnly: true, MaxTurns: 6})
			if err != nil {
				t.Fatalf("live read-only run: %v", err)
			}
			if strings.TrimSpace(result.Output) == "" || result.SessionID == "" {
				t.Fatalf("live result = %+v, want output and session ID", result)
			}
			got, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "original\n" {
				t.Fatalf("sentinel = %q, want unchanged original", got)
			}
			t.Logf("native denial evidence reported through Result.Output: %s", result.Output)
		})
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
