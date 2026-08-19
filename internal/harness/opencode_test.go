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
	base := []string{"run", "--dangerously-skip-permissions"}

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

	withLegacySettings := opencodeArgs(Request{Prompt: "rendered prompt", Effort: "high", MaxTurns: 30}, []string{"--format", "json"})
	want = append(append([]string{}, base...), "--format", "json", "rendered prompt")
	if !reflect.DeepEqual(withLegacySettings, want) {
		t.Errorf("with legacy settings = %v, want %v (no OpenCode effort or turn flags)", withLegacySettings, want)
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

	writeOpenCodeScript(t, bin, `
[ "$1" = run ] || exit 11
[ "$2" = --dangerously-skip-permissions ] || exit 12
[ "$3" = --model ] || exit 13
[ "$4" = openai/gpt-5 ] || exit 14
[ "$5" = "rendered prompt" ] || exit 15
[ "$PWD" = "$EXPECTED_DIR" ] || exit 16
mkdir -p .romp
printf '%s\n' '---' 'title: OpenCode result' 'commit: feat: add result' '---' '' 'OpenCode completed the task.' > .romp/pull-request.md
printf '%s\n' 'OpenCode completed the task.'
`)

	result, err := (OpenCode{}).Run(context.Background(), Request{
		Dir:    worktree,
		Prompt: "rendered prompt",
		Model:  "openai/gpt-5",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "OpenCode completed the task.\n" {
		t.Errorf("Output = %q, want command output", result.Output)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".romp", "pull-request.md")); err != nil {
		t.Fatalf("result artifact: %v", err)
	}
}

func TestOpenCodeRunReturnsOutputOnFailure(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeOpenCodeScript(t, bin, "printf '%s\\n' 'agent failed' >&2\nexit 17")

	result, err := (OpenCode{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if err == nil {
		t.Fatal("Run() = nil error, want command failure")
	}
	if !strings.Contains(err.Error(), "exit status 17") || !strings.Contains(err.Error(), "agent failed") {
		t.Errorf("Run error = %q, want exit status and output", err)
	}
	if result.Output != "agent failed\n" {
		t.Errorf("Output = %q, want command output", result.Output)
	}
}

func writeOpenCodeScript(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "opencode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}
