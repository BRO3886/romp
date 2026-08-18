package harness

import (
	"reflect"
	"testing"
)

func TestCodexArgs(t *testing.T) {
	base := []string{"exec", "--sandbox", "workspace-write", "--color", "never"}

	plain := codexArgs(Request{Prompt: "hello"}, nil)
	if !reflect.DeepEqual(plain, base) {
		t.Errorf("plain = %v, want %v (prompt stays off argv)", plain, base)
	}

	withDir := codexArgs(Request{Prompt: "hello", Dir: "/tmp/wt"}, nil)
	want := append(append([]string{}, base...), "--cd", "/tmp/wt")
	if !reflect.DeepEqual(withDir, want) {
		t.Errorf("with dir = %v, want %v", withDir, want)
	}

	withModel := codexArgs(Request{Prompt: "hello", Model: "gpt-5.6-terra"}, nil)
	want = append(append([]string{}, base...), "--model", "gpt-5.6-terra")
	if !reflect.DeepEqual(withModel, want) {
		t.Errorf("with model = %v, want %v", withModel, want)
	}

	withEffort := codexArgs(Request{Prompt: "hello", Effort: "high"}, nil)
	want = append(append([]string{}, base...), "-c", "model_reasoning_effort=high")
	if !reflect.DeepEqual(withEffort, want) {
		t.Errorf("with effort = %v, want %v", withEffort, want)
	}

	both := codexArgs(Request{Prompt: "hello", Dir: "/tmp/wt", Model: "gpt-5.6-terra", Effort: "xhigh"}, []string{"--ephemeral"})
	want = append(append([]string{}, base...), "--cd", "/tmp/wt", "--model", "gpt-5.6-terra", "-c", "model_reasoning_effort=xhigh", "--ephemeral")
	if !reflect.DeepEqual(both, want) {
		t.Errorf("dir model and effort = %v, want %v", both, want)
	}

	// MaxTurns has no codex exec equivalent; the adapter must not invent one.
	withTurns := codexArgs(Request{Prompt: "hello", MaxTurns: 30}, nil)
	if !reflect.DeepEqual(withTurns, base) {
		t.Errorf("with max turns = %v, want %v (no max-turns flag)", withTurns, base)
	}
}

func TestCodexName(t *testing.T) {
	if got := (Codex{}).Name(); got != "codex" {
		t.Errorf("Name() = %q, want codex", got)
	}
}
