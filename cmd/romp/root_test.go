package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
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
	cmd.SetArgs([]string{"--verify", "go build ./...", "--harness", "codex", "--model", "opus", "--effort", "low", "--width", "1", "-i", "7"})
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
	if gotOverrides.Effort != "low" {
		t.Errorf("overrides.Effort = %q, want low", gotOverrides.Effort)
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
	h, err := buildHarness("claude")
	if err != nil {
		t.Errorf("buildHarness(claude) = %v", err)
	} else if h.Name() != "claude" {
		t.Errorf("buildHarness(claude).Name() = %q, want claude", h.Name())
	}
	h, err = buildHarness("codex")
	if err != nil {
		t.Errorf("buildHarness(codex) = %v", err)
	} else if h.Name() != "codex" {
		t.Errorf("buildHarness(codex).Name() = %q, want codex", h.Name())
	}
	if _, err := buildHarness("bogus"); err == nil {
		t.Error("buildHarness(bogus) = nil error, want unknown-harness")
	}
}

func TestLoadTemplate(t *testing.T) {
	root := t.TempDir()

	got, err := loadTemplate(root, "")
	if err != nil {
		t.Fatalf("loadTemplate default: %v", err)
	}
	if got != prompt.Default() {
		t.Error("loadTemplate default = custom text, want prompt.Default()")
	}

	if err := os.WriteFile(filepath.Join(root, "custom.md"), []byte("CUSTOM {{.Issue}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadTemplate(root, "custom.md")
	if err != nil {
		t.Fatalf("loadTemplate custom: %v", err)
	}
	if got != "CUSTOM {{.Issue}}" {
		t.Errorf("loadTemplate custom = %q", got)
	}

	if _, err := loadTemplate(root, "missing.md"); err == nil {
		t.Error("loadTemplate missing = nil error, want error")
	}
}

func TestResolveBrief(t *testing.T) {
	root := t.TempDir()

	if got, err := resolveBrief(root, ""); err != nil || got != "" {
		t.Errorf("resolveBrief empty = %q, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(root, "DESIGN.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBrief(root, "DESIGN.md")
	if err != nil || got != "DESIGN.md" {
		t.Errorf("resolveBrief = %q, %v", got, err)
	}

	if _, err := resolveBrief(root, "nope.md"); err == nil {
		t.Error("resolveBrief missing = nil error, want error")
	}
}

func TestBuildRunnerWiresConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DESIGN.md"), []byte("brief"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Harness.MaxTurns = 7
	cfg.Scope.Ignore = []string{"vendor/**"}
	cfg.Prompt.Template = "prompt.md"
	cfg.Prompt.Brief = "DESIGN.md"

	r, err := buildRunner(root, &cfg, []string{"go test ./..."}, harness.Claude{}, time.Minute)
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	if r.MaxTurns != 7 {
		t.Errorf("MaxTurns = %d, want 7", r.MaxTurns)
	}
	if len(r.Ignore) != 1 || r.Ignore[0] != "vendor/**" {
		t.Errorf("Ignore = %v, want [vendor/**]", r.Ignore)
	}
	if r.Brief != "DESIGN.md" {
		t.Errorf("Brief = %q, want DESIGN.md", r.Brief)
	}
	if r.Prompt.Template != "custom" {
		t.Errorf("Prompt.Template = %q, want custom", r.Prompt.Template)
	}
}
