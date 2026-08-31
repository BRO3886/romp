package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/config"
)

func TestSeedConfig(t *testing.T) {
	got := seedConfig([]string{"go build ./...", "go test ./... -count=1"})
	for _, want := range []string{
		`commands = [`,
		`"go build ./...",`,
		`"go test ./... -count=1",`,
		`default = "codex"`,
		`claimed_label  = "romp:claimed"`,
		`blocked_label  = "romp:blocked"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seedConfig missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "timeout") {
		t.Errorf("seedConfig contains a timeout:\n%s", got)
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
	if len(cfg.Verify.Commands) != 2 || cfg.Verify.Commands[0] != "go build ./..." || cfg.Verify.Commands[1] != "go test ./... -count=1" {
		t.Errorf("round-trip verify = %+v", cfg.Verify)
	}
	if cfg.ClaimedLabel != "romp:claimed" || cfg.BlockedLabel != "romp:blocked" {
		t.Errorf("round-trip labels = %q / %q", cfg.ClaimedLabel, cfg.BlockedLabel)
	}
}

func TestSeedConfigNoBuild(t *testing.T) {
	got := seedConfig([]string{"npm test"})
	if !strings.Contains(got, `"npm test",`) {
		t.Errorf("seedConfig missing command:\n%s", got)
	}
}

func TestInitLabels(t *testing.T) {
	cfg := config.Defaults()
	got := initLabels(&cfg)
	if len(got) != 3 {
		t.Fatalf("initLabels = %d labels, want 3", len(got))
	}
	if got[0].name != "romp" || got[0].desc == "" {
		t.Errorf("trigger = %+v, want named romp with a description", got[0])
	}
	if got[1].name != "romp:claimed" || got[1].desc == "" {
		t.Errorf("claimed = %+v, want named romp:claimed with a description", got[1])
	}
	if got[2].name != "romp:blocked" || got[2].desc == "" {
		t.Errorf("blocked = %+v, want named romp:blocked with a description", got[2])
	}

	cfg.Label, cfg.ClaimedLabel, cfg.BlockedLabel = "work", "taken", "stuck"
	got = initLabels(&cfg)
	if got[0].name != "work" || got[1].name != "taken" || got[2].name != "stuck" {
		t.Errorf("custom names = %q %q %q", got[0].name, got[1].name, got[2].name)
	}
	if got[0].desc == "" || got[1].desc == "" || got[2].desc == "" {
		t.Errorf("custom labels missing descriptions: %+v", got)
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
