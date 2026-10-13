package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/job"
)

func TestForegroundFlagsExecuteInDaemonAndPersistOutcome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := job.Open(job.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	testDaemon(t, store)
	root := t.TempDir()
	for _, args := range [][]string{{"init", root}, {"-C", root, "remote", "add", "origin", "https://github.com/fixture/repo.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "romp.toml"), []byte("base = 'invalid branch'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	cmd := newRemoteRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-i", "7", "--verify", "true", "--harness", "codex", "--model", "fixture-model", "--effort", "high", "--no-review"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid base branch") {
		t.Fatalf("foreground result=%v output=%s", err, out.String())
	}
	history, err := store.History(context.Background(), "fixture/repo", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Outcome != "error" || history[0].BuilderHarness != "codex" || history[0].ReviewerHarness != "" {
		t.Fatalf("foreground history=%+v", history)
	}
	active, err := store.List(context.Background(), "")
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestForegroundRefusesMissingVerificationBeforeClaim(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := job.Open(job.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	testDaemon(t, store)
	root := t.TempDir()
	for _, args := range [][]string{{"init", root}, {"-C", root, "remote", "add", "origin", "https://github.com/fixture/repo.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	t.Chdir(root)
	cmd := newRemoteRunCmd()
	cmd.SetArgs([]string{"-i", "7"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no verify command") {
		t.Fatalf("verification preflight=%v", err)
	}
	jobs, err := store.List(context.Background(), "")
	if err != nil || len(jobs) != 0 {
		t.Fatal(jobs, err)
	}
}
