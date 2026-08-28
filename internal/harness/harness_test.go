package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFailuresPreserveSeparateDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, Request) (Result, error)
	}{
		{name: "claude", run: (Claude{}).Run},
		{name: "codex", run: (Codex{}).Run},
		{name: "opencode", run: (OpenCode{}).Run},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			writeHarnessScript(t, bin, tt.name, `
printf '%s\n' 'raw stdout diagnostic'
printf '%s\n' 'raw stderr diagnostic' >&2
exit 17
`)
			result, err := tt.run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
			if err == nil {
				t.Fatal("Run error = nil, want command failure")
			}
			for _, want := range []string{"exit status 17", "stdout:\nraw stdout diagnostic", "stderr:\nraw stderr diagnostic"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Run error = %q, want %q", err, want)
				}
			}
			if result != (Result{}) {
				t.Errorf("Result = %+v, want empty for unstructured failure", result)
			}
		})
	}
}

func TestStructuredRunFailureDoesNotExposeSessionID(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		run     func(context.Context, Request) (Result, error)
	}{
		{name: "claude", fixture: "claude-2.1.235-success.json", run: (Claude{}).Run},
		{name: "codex", fixture: "codex-0.147.0-success.jsonl", run: (Codex{}).Run},
		{name: "opencode", fixture: "opencode-1.18.18-success.jsonl", run: (OpenCode{}).Run},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			fixture, err := filepath.Abs(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FIXTURE", fixture)
			writeHarnessScript(t, bin, tt.name, "cat \"$FIXTURE\"\nexit 17\n")

			result, err := tt.run(context.Background(), Request{Dir: t.TempDir(), Prompt: "rendered prompt"})
			if err == nil {
				t.Fatal("Run error = nil, want command failure")
			}
			if result != (Result{}) {
				t.Errorf("Run result = %+v, want empty result", result)
			}
		})
	}
}
