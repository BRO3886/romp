package harness

import (
	"reflect"
	"testing"
)

func TestClaudeArgs(t *testing.T) {
	base := []string{"-p", "--output-format", "text", "--permission-mode", "bypassPermissions"}

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
