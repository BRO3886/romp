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
}
