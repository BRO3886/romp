package main

import (
	"context"
	"testing"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/runner"
)

func TestRunCmdFlags(t *testing.T) {
	var (
		gotOverrides config.Overrides
		gotVerify    string
		gotVerifySet bool
		gotIssue     int
		ran          bool
	)
	factory := func(_ context.Context, o config.Overrides, verifyFlag string, verifySet bool) (*runner.Runner, error) {
		gotOverrides, gotVerify, gotVerifySet = o, verifyFlag, verifySet
		return &runner.Runner{}, nil
	}
	run := func(_ context.Context, _ *runner.Runner, issue int) error {
		gotIssue, ran = issue, true
		return nil
	}

	cmd := newRunCmd(factory, run)
	cmd.SetArgs([]string{"--verify", "go build ./...", "--harness", "codex", "--model", "opus", "--width", "1", "-i", "7"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !ran {
		t.Fatal("run was never called")
	}
	if gotIssue != 7 {
		t.Errorf("issue = %d, want 7", gotIssue)
	}
	if !gotVerifySet {
		t.Error("verifySet = false, want true")
	}
	if gotVerify != "go build ./..." {
		t.Errorf("verifyFlag = %q, want go build ./...", gotVerify)
	}
	if gotOverrides.Harness != "codex" {
		t.Errorf("overrides.Harness = %q, want codex", gotOverrides.Harness)
	}
	if gotOverrides.Model != "opus" {
		t.Errorf("overrides.Model = %q, want opus", gotOverrides.Model)
	}
	if gotOverrides.Width != 1 {
		t.Errorf("overrides.Width = %d, want 1", gotOverrides.Width)
	}
}

func TestRunCmdVerifyDefaults(t *testing.T) {
	var (
		gotVerify    string
		gotVerifySet bool
	)
	factory := func(_ context.Context, _ config.Overrides, verifyFlag string, verifySet bool) (*runner.Runner, error) {
		gotVerify, gotVerifySet = verifyFlag, verifySet
		return &runner.Runner{}, nil
	}
	run := func(_ context.Context, _ *runner.Runner, _ int) error { return nil }

	cmd := newRunCmd(factory, run)
	cmd.SetArgs([]string{"-i", "7"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotVerifySet {
		t.Error("verifySet = true, want false when --verify is absent")
	}
	if gotVerify != "" {
		t.Errorf("verifyFlag = %q, want empty", gotVerify)
	}
}

func TestVerifyCommands(t *testing.T) {
	cfg := &config.Config{
		Verify: config.Verify{Build: "go build ./...", Test: "go test ./... -count=1", Lint: "golangci-lint run"},
	}

	got, err := verifyCommands(cfg, "", false)
	if err != nil {
		t.Fatalf("verifyCommands: %v", err)
	}
	want := []string{"go build ./...", "go test ./... -count=1", "golangci-lint run"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("verifyCommands = %v, want %v", got, want)
	}

	got, err = verifyCommands(cfg, "go build ./...", true)
	if err != nil {
		t.Fatalf("verifyCommands flag: %v", err)
	}
	if len(got) != 1 || got[0] != "go build ./..." {
		t.Errorf("verifyCommands flag = %v, want [go build ./...]", got)
	}

	if _, err := verifyCommands(&config.Config{}, "", false); err == nil {
		t.Error("verifyCommands() empty = nil error, want error")
	}
}

func TestBuildHarness(t *testing.T) {
	if _, err := buildHarness("claude"); err != nil {
		t.Errorf("buildHarness(claude) = %v", err)
	}
	if _, err := buildHarness("codex"); err == nil {
		t.Error("buildHarness(codex) = nil error, want not-implemented")
	}
	if _, err := buildHarness("bogus"); err == nil {
		t.Error("buildHarness(bogus) = nil error, want unknown-harness")
	}
}
