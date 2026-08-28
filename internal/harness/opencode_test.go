package harness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCodeArgs(t *testing.T) {
	base := []string{"run", "--auto", "--format", "json"}

	plain := opencodeArgs(Request{Prompt: "rendered prompt"}, nil)
	want := append(append([]string{}, base...), "rendered prompt")
	if !reflect.DeepEqual(plain, want) {
		t.Errorf("plain = %v, want %v", plain, want)
	}

	withModel := opencodeArgs(Request{Prompt: "rendered prompt", Model: "openai/gpt-5"}, nil)
	want = append(append([]string{}, base...), "--model", "openai/gpt-5", "rendered prompt")
	if !reflect.DeepEqual(withModel, want) {
		t.Errorf("with model = %v, want %v", withModel, want)
	}

	withVariant := opencodeArgs(Request{Prompt: "rendered prompt", Effort: "high"}, nil)
	want = append(append([]string{}, base...), "--variant", "high", "rendered prompt")
	if !reflect.DeepEqual(withVariant, want) {
		t.Errorf("with variant = %v, want %v", withVariant, want)
	}

	withModelAndVariant := opencodeArgs(Request{Prompt: "rendered prompt", Model: "openai/gpt-5", Effort: "high"}, nil)
	want = append(append([]string{}, base...), "--model", "openai/gpt-5", "--variant", "high", "rendered prompt")
	if !reflect.DeepEqual(withModelAndVariant, want) {
		t.Errorf("with model and variant = %v, want %v", withModelAndVariant, want)
	}

	withLegacySettings := opencodeArgs(Request{Prompt: "rendered prompt", MaxTurns: 30}, nil)
	want = append(append([]string{}, base...), "rendered prompt")
	if !reflect.DeepEqual(withLegacySettings, want) {
		t.Errorf("with legacy settings = %v, want %v (no OpenCode turn flag)", withLegacySettings, want)
	}
}

func TestParseOpenCodeResultRecordedOutput(t *testing.T) {
	out, err := os.ReadFile("testdata/opencode-1.18.18-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	result, err := parseOpenCodeResult(out)
	if err != nil {
		t.Fatalf("parseOpenCodeResult: %v", err)
	}
	if result.SessionID != "ses_65b3acf58ffeLSa4dfj1RVoPpW" {
		t.Errorf("SessionID = %q", result.SessionID)
	}
	if result.Output != "Applied the requested changes.\nOpenCode completed the task." {
		t.Errorf("Output = %q, want final assistant text", result.Output)
	}
}

func TestParseOpenCodeResultRejectsIncompleteStreams(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
	}{
		{name: "session only", fixture: "opencode-1.18.18-session-only.jsonl"},
		{name: "step only", fixture: "opencode-1.18.18-step-only.jsonl"},
		{name: "text without completed step", fixture: "opencode-1.18.18-text-only.jsonl"},
		{name: "empty text", fixture: "opencode-1.18.18-empty-text.jsonl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			result, err := parseOpenCodeResult(out)
			if err == nil {
				t.Fatalf("parseOpenCodeResult = %+v, nil error; want rejection", result)
			}
			if result != (Result{}) {
				t.Errorf("parseOpenCodeResult result = %+v, want empty result", result)
			}
		})
	}
}

func TestOpenCodeName(t *testing.T) {
	if got := (OpenCode{}).Name(); got != "opencode" {
		t.Errorf("Name() = %q, want opencode", got)
	}
}

func TestOpenCodeRunCreatesResultArtifact(t *testing.T) {
	bin := t.TempDir()
	worktree := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EXPECTED_DIR", worktree)

	writeHarnessScript(t, bin, "opencode", `
[ "$1" = run ] || exit 11
[ "$2" = --auto ] || exit 12
[ "$3" = --format ] || exit 13
[ "$4" = json ] || exit 14
[ "$5" = --model ] || exit 15
[ "$6" = openai/gpt-5 ] || exit 16
[ "$7" = "rendered prompt" ] || exit 17
[ "$PWD" = "$EXPECTED_DIR" ] || exit 16
mkdir -p .romp
printf '%s\n' '---' 'title: OpenCode result' 'commit: feat: add result' '---' '' 'OpenCode completed the task.' > .romp/pull-request.md
printf '%s\n' '{"type":"step_start","sessionID":"ses_test","part":{"type":"step-start"}}' '{"type":"text","sessionID":"ses_test","part":{"type":"text","text":"OpenCode completed the task."}}' '{"type":"step_finish","sessionID":"ses_test","part":{"type":"step-finish"}}'
printf '%s\n' 'OpenCode warning on stderr' >&2
`)

	result, err := (OpenCode{}).Run(context.Background(), Request{
		Dir:    worktree,
		Prompt: "rendered prompt",
		Model:  "openai/gpt-5",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "OpenCode completed the task." {
		t.Errorf("Output = %q, want command output", result.Output)
	}
	if result.SessionID != "ses_test" {
		t.Errorf("SessionID = %q, want ses_test", result.SessionID)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".romp", "pull-request.md")); err != nil {
		t.Fatalf("result artifact: %v", err)
	}
}

func TestOpenCodeRunReturnsOutputOnFailure(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeHarnessScript(t, bin, "opencode", "printf '%s\\n' 'agent failed' >&2\nexit 17")

	result, err := (OpenCode{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if err == nil {
		t.Fatal("Run() = nil error, want command failure")
	}
	if !strings.Contains(err.Error(), "exit status 17") || !strings.Contains(err.Error(), "agent failed") {
		t.Errorf("Run error = %q, want exit status and output", err)
	}
	if result.Output != "" {
		t.Errorf("Output = %q, want no structured assistant output", result.Output)
	}
}

func writeHarnessScript(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}
