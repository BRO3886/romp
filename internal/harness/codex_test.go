package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCodexRunUsesAppServerProtocol(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_FILE", argsFile)
	writeHarnessScript(t, bin, "codex", `
printf '%s\n' "$@" > "$ARGS_FILE"
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%s\n' '{"id":1,"result":{"userAgent":"codex"}}'
      ;;
    *'"method":"thread/start"'*)
      printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1","sessionId":"session-1"},"instructionSources":["/worktree/AGENTS.md"]}}'
      ;;
    *'"method":"turn/start"'*)
      printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}'
      printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"type":"agentMessage","text":"Codex completed the task.","phase":"final_answer"}}}'
      printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","error":null}}}'
      ;;
  esac
done
printf '%s\n' 'Codex warning on stderr' >&2
`)

	result, err := (Codex{Args: []string{"--strict-config"}}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "Codex completed the task." || result.SessionID != "session-1" {
		t.Fatalf("result = %+v", result)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "app-server\n--listen\nstdio://\n--strict-config\n" {
		t.Fatalf("codex arguments = %q", args)
	}
}

func TestCodexRunReportsProtocolFailureWithStderr(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeHarnessScript(t, bin, "codex", `
IFS= read -r line
printf '%s\n' 'not-json'
printf '%s\n' 'server diagnostic' >&2
sleep 10
`)

	result, err := (Codex{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if err == nil || !strings.Contains(err.Error(), "decode message") || !strings.Contains(err.Error(), "server diagnostic") {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestCodexRunPreservesCancellation(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeHarnessScript(t, bin, "codex", `
IFS= read -r line
sleep 10
`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := (Codex{}).Run(ctx, Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("result = %+v, error = %v, want deadline exceeded", result, err)
	}
}

func TestCodexName(t *testing.T) {
	if got := (Codex{}).Name(); got != "codex" {
		t.Fatalf("Name() = %q, want codex", got)
	}
}

func TestLiveCodexAppServerHandshake(t *testing.T) {
	if os.Getenv("ROMP_LIVE_CODEX_TESTS") != "1" {
		t.Skip("set ROMP_LIVE_CODEX_TESTS=1 to test the installed app-server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process, err := startCodexServer(ctx, t.TempDir(), codexServerArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	failed := true
	defer func() {
		if err := process.shutdown(failed); err != nil {
			t.Errorf("shutdown: %v\nstderr:\n%s", err, process.stderr.String())
		}
	}()

	client := newRPCClient(process)
	var initialized json.RawMessage
	if err := client.call("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "romp_test", "title": "Romp Test", "version": "0.1"},
	}, &initialized, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	var skills struct {
		Data []struct {
			Skills []struct {
				Name  string `json:"name"`
				Path  string `json:"path"`
				Scope string `json:"scope"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := client.call("skills/list", map[string]any{"cwds": []string{t.TempDir()}, "forceReload": true}, &skills, nil); err != nil {
		t.Fatal(err)
	}
	if skills.Data == nil {
		t.Fatal("skills/list returned nil data")
	}
	for _, entry := range skills.Data {
		for _, skill := range entry.Skills {
			if skill.Name == "siddhartha-go" {
				t.Logf("siddhartha-go scope=%s path=%s", skill.Scope, skill.Path)
			}
		}
	}
	failed = false
}

func TestLiveCodexAppServerRun(t *testing.T) {
	if os.Getenv("ROMP_LIVE_CODEX_TESTS") != "1" {
		t.Skip("set ROMP_LIVE_CODEX_TESTS=1 to run an authenticated app-server turn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := (Codex{Ephemeral: true}).Run(ctx, Request{
		Dir:      t.TempDir(),
		Prompt:   "Use the supplied Go skill. Reply with exactly server-ok and do not use tools.",
		Effort:   "low",
		Skills:   []string{"siddhartha-go"},
		ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != "server-ok" || result.SessionID == "" || result.Metadata == nil || !slices.Equal(result.Metadata.Skills, []string{"siddhartha-go"}) {
		t.Fatalf("result = %+v", result)
	}
}
