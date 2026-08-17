package main

import (
	"context"
	"testing"

	"github.com/BRO3886/romp/internal/runner"
)

func TestRunCmdVerifyFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default", []string{"-i", "7"}, "go test ./... -count=1"},
		{"override", []string{"--verify", "go build ./...", "-i", "7"}, "go build ./..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				got   *runner.Runner
				issue int
			)
			cmd := newRunCmd(func(_ context.Context, r *runner.Runner, i int) error {
				got, issue = r, i
				return nil
			})
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute %v: %v", tt.args, err)
			}
			if got == nil {
				t.Fatal("run was never called")
			}
			if got.Verify != tt.want {
				t.Errorf("Runner.Verify = %q, want %q", got.Verify, tt.want)
			}
			if issue != 7 {
				t.Errorf("issue = %d, want 7", issue)
			}
		})
	}
}
