package harness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaudeArgs(t *testing.T) {
	base := []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions"}

	withModel := claudeArgs(Request{Prompt: "hello", Model: "sonnet"}, nil)
	want := append(append([]string{}, base...), "--model", "sonnet", "hello")
	if !reflect.DeepEqual(withModel, want) {
		t.Errorf("with model = %v, want %v", withModel, want)
	}

	noModel := claudeArgs(Request{Prompt: "hello"}, nil)
	want = append(append([]string{}, base...), "hello")
	if !reflect.DeepEqual(noModel, want) {
		t.Errorf("no model = %v, want %v", noModel, want)
	}

	withEffort := claudeArgs(Request{Prompt: "hello", Effort: "high"}, nil)
	want = append(append([]string{}, base...), "--effort", "high", "hello")
	if !reflect.DeepEqual(withEffort, want) {
		t.Errorf("with effort = %v, want %v", withEffort, want)
	}

	both := claudeArgs(Request{Prompt: "hello", Model: "sonnet", Effort: "max"}, []string{"--verbose"})
	want = append(append([]string{}, base...), "--model", "sonnet", "--effort", "max", "--verbose", "hello")
	if !reflect.DeepEqual(both, want) {
		t.Errorf("model and effort = %v, want %v", both, want)
	}

	withTurns := claudeArgs(Request{Prompt: "hello", MaxTurns: 30}, nil)
	want = append(append([]string{}, base...), "--max-turns", "30", "hello")
	if !reflect.DeepEqual(withTurns, want) {
		t.Errorf("with max turns = %v, want %v", withTurns, want)
	}

	noTurns := claudeArgs(Request{Prompt: "hello"}, nil)
	want = append(append([]string{}, base...), "hello")
	if !reflect.DeepEqual(noTurns, want) {
		t.Errorf("zero max turns = %v, want %v (no --max-turns flag)", noTurns, want)
	}
}

func TestClaudeRunParsesStructuredOutputWithStderrDiagnostic(t *testing.T) {
	bin := t.TempDir()
	fixture, err := filepath.Abs("testdata/claude-2.1.235-success.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE", fixture)
	writeHarnessScript(t, bin, "claude", `
while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "$line"; done < "$FIXTURE"
printf '%s\n' 'Claude warning on stderr' >&2
`)

	result, err := (Claude{}).Run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "Claude completed the task." || result.SessionID != "902816de-f8a8-402b-a198-242830f8d818" {
		t.Errorf("result = %+v", result)
	}
}

func TestParseClaudeResultRecordedOutput(t *testing.T) {
	out, err := os.ReadFile("testdata/claude-2.1.235-success.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := parseClaudeResult(out)
	if err != nil {
		t.Fatalf("parseClaudeResult: %v", err)
	}
	if result.SessionID != "902816de-f8a8-402b-a198-242830f8d818" {
		t.Errorf("SessionID = %q", result.SessionID)
	}
	if result.Output != "Claude completed the task." {
		t.Errorf("Output = %q, want final assistant text", result.Output)
	}
}
