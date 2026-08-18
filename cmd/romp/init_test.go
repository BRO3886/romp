package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/config"
)

func TestSeedConfig(t *testing.T) {
	got := seedConfig("go build ./...", "go test ./... -count=1", "go")
	for _, want := range []string{
		`build = "go build ./..."`,
		`test  = "go test ./... -count=1"`,
		`default = "claude"`,
		`claimed_label  = "romp:claimed"`,
		`blocked_label  = "romp:blocked"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seedConfig missing %q:\n%s", want, got)
		}
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "romp.toml"), []byte(got), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		t.Fatalf("seedConfig output does not parse: %v", err)
	}
	if cfg.Verify.Build != "go build ./..." || cfg.Verify.Test != "go test ./... -count=1" {
		t.Errorf("round-trip verify = %+v", cfg.Verify)
	}
	if cfg.ClaimedLabel != "romp:claimed" || cfg.BlockedLabel != "romp:blocked" {
		t.Errorf("round-trip labels = %q / %q", cfg.ClaimedLabel, cfg.BlockedLabel)
	}
}

func TestSeedConfigNoBuild(t *testing.T) {
	got := seedConfig("", "npm test", "node")
	if strings.Contains(got, "build") {
		t.Errorf("seedConfig should omit build when empty:\n%s", got)
	}
	if !strings.Contains(got, `test  = "npm test"`) {
		t.Errorf("seedConfig missing test:\n%s", got)
	}
}

func TestEnsureGitignore(t *testing.T) {
	t.Run("creates", func(t *testing.T) {
		root := t.TempDir()
		if err := ensureGitignore(root); err != nil {
			t.Fatalf("ensureGitignore: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != ".romp/local.toml\n" {
			t.Errorf(".gitignore = %q", string(data))
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n.romp/local.toml\n"), 0o644)
		if err := ensureGitignore(root); err != nil {
			t.Fatalf("ensureGitignore: %v", err)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
		if got := strings.Count(string(data), ".romp/local.toml"); got != 1 {
			t.Errorf(".gitignore has %d entries, want 1", got)
		}
	})

	t.Run("appends without trailing newline", func(t *testing.T) {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/"), 0o644)
		if err := ensureGitignore(root); err != nil {
			t.Fatalf("ensureGitignore: %v", err)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
		if string(data) != "bin/\n.romp/local.toml\n" {
			t.Errorf(".gitignore = %q", string(data))
		}
	})
}
