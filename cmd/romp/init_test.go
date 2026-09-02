package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/config"
)

func TestSeedConfig(t *testing.T) {
	got := seedConfig([]string{"go build ./...", "go test ./... -count=1"}, true)
	for _, want := range []string{
		`commands = [`,
		`"go build ./...",`,
		`"go test ./... -count=1",`,
		`default = "codex"`,
		`claimed_label  = "romp:claimed"`,
		`blocked_label  = "romp:blocked"`,
		`changes_requested_label = "romp:changes-requested"`,
		`[review]`,
		`enabled = true`,
		`max_fix_rounds = 2`,
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
	if cfg.ClaimedLabel != "romp:claimed" || cfg.BlockedLabel != "romp:blocked" || cfg.ChangesRequestedLabel != "romp:changes-requested" {
		t.Errorf("round-trip labels = %q / %q / %q", cfg.ClaimedLabel, cfg.BlockedLabel, cfg.ChangesRequestedLabel)
	}
	if !cfg.Review.Enabled {
		t.Error("round-trip Review.Enabled = false, want true")
	}
	if cfg.Review.MaxFixRounds != 2 {
		t.Errorf("round-trip Review.MaxFixRounds = %d, want 2", cfg.Review.MaxFixRounds)
	}
}

func TestSeedConfigNoBuild(t *testing.T) {
	got := seedConfig([]string{"npm test"}, false)
	if !strings.Contains(got, `"npm test",`) {
		t.Errorf("seedConfig missing command:\n%s", got)
	}
	if !strings.Contains(got, "[review]\nenabled = false") {
		t.Errorf("seedConfig missing disabled review setting:\n%s", got)
	}
}

func TestChooseReviewEnabled(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "default", input: "\n", want: true},
		{name: "yes", input: "y\n", want: true},
		{name: "no", input: "n\n", want: false},
		{name: "retry invalid", input: "maybe\nno\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := chooseReviewEnabled(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("chooseReviewEnabled: %v", err)
			}
			if got != tt.want {
				t.Errorf("chooseReviewEnabled = %v, want %v", got, tt.want)
			}
			if !strings.Contains(out.String(), "Enable review gate? [Y/n]") {
				t.Errorf("prompt = %q", out.String())
			}
		})
	}
}

func TestInitLabels(t *testing.T) {
	cfg := config.Defaults()
	got := initLabels(&cfg)
	if len(got) != 4 {
		t.Fatalf("initLabels = %d labels, want 4", len(got))
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
	if got[3].name != "romp:changes-requested" || got[3].desc == "" {
		t.Errorf("changes requested = %+v, want named label with a description", got[3])
	}

	cfg.Label, cfg.ClaimedLabel, cfg.BlockedLabel, cfg.ChangesRequestedLabel = "work", "taken", "stuck", "review"
	got = initLabels(&cfg)
	if got[0].name != "work" || got[1].name != "taken" || got[2].name != "stuck" || got[3].name != "review" {
		t.Errorf("custom names = %q %q %q %q", got[0].name, got[1].name, got[2].name, got[3].name)
	}
	if got[0].desc == "" || got[1].desc == "" || got[2].desc == "" || got[3].desc == "" {
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
