package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Label != "romp" || cfg.ClaimedLabel != "romp:claimed" || cfg.BlockedLabel != "romp:blocked" ||
		cfg.Width != 3 || cfg.Timeout != "" || cfg.HistoryDays != 30 ||
		cfg.Harness.Default != "codex" || cfg.Harness.Effort != "high" || !cfg.Review.Enabled {
		t.Fatalf("Defaults() = %+v", cfg)
	}
	if cfg.ReviewModel() != cfg.Harness.Model || cfg.ReviewHarness() != cfg.Harness.Default {
		t.Fatalf("review fallback = model %q, harness %q; want builder settings", cfg.ReviewModel(), cfg.ReviewHarness())
	}
}

func TestLoadReviewPrecedenceAndFallback(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	write(t, filepath.Join(user, "romp", "config.toml"), "[review]\nenabled = true\nmodel = \"reviewer-model\"\nharness = \"claude\"\n")

	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "[review]\nenabled = false\nmodel = \"\"\nharness = \"\"\n")
	write(t, filepath.Join(root, ".romp", "local.toml"), "[harness]\ndefault = \"codex\"\nmodel = \"builder-model\"\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Review.Enabled {
		t.Error("Review.Enabled = true, want repo config to disable review")
	}
	if cfg.Review.Model != "" || cfg.Review.Harness != "" {
		t.Errorf("Review = %+v, want explicit empty repo values", cfg.Review)
	}
	if cfg.ReviewModel() != "builder-model" {
		t.Errorf("ReviewModel() = %q, want builder-model", cfg.ReviewModel())
	}
	if cfg.ReviewHarness() != "codex" {
		t.Errorf("ReviewHarness() = %q, want codex", cfg.ReviewHarness())
	}
}

func TestLoadNoReviewOverrideIsNotPersisted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "romp.toml")
	write(t, path, "[review]\nenabled = true\n")

	cfg, err := Load(root, Overrides{NoReview: true})
	if err != nil {
		t.Fatalf("Load override: %v", err)
	}
	if cfg.Review.Enabled {
		t.Error("Review.Enabled = true, want --no-review override")
	}

	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	if !cfg.Review.Enabled {
		t.Error("Review.Enabled = false after reload, override was persisted")
	}
}

func TestLoadRejectsUnknownReviewKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "[review]\nenabld = true\n")

	_, err := Load(root, Overrides{})
	if err == nil {
		t.Fatal("Load() = nil error, want unknown review key error")
	}
	if !strings.Contains(err.Error(), "unknown key review.enabld") {
		t.Errorf("err = %q, want unknown review.enabld", err)
	}
}

func TestLoadIgnoresUnrelatedUnknownKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "label = \"work\"\nfuture_setting = true\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load unrelated unknown key: %v", err)
	}
	if cfg.Label != "work" {
		t.Errorf("Label = %q, want work", cfg.Label)
	}
}

func TestLoadRejectsUnknownReviewHarness(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "[review]\nharness = \"bogus\"\n")

	_, err := Load(root, Overrides{})
	if err == nil {
		t.Fatal("Load() = nil error, want unknown review harness")
	}
	want := `review.harness: unknown harness "bogus" (want claude, codex, or opencode)`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestLoadNoFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load(t.TempDir(), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != "romp" || cfg.Width != 3 || cfg.Timeout != "" {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
}

func TestLoadTimeoutPrecedence(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	write(t, filepath.Join(user, "romp", "config.toml"), "timeout = \"25m\"\n")

	root := t.TempDir()
	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load user timeout: %v", err)
	}
	if cfg.Timeout != "25m" {
		t.Errorf("Timeout = %q, want 25m from user config", cfg.Timeout)
	}

	write(t, filepath.Join(root, "romp.toml"), "timeout = \"1.5h\"\n")
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load repository timeout: %v", err)
	}
	if cfg.Timeout != "1.5h" {
		t.Errorf("Timeout = %q, want 1.5h from repository config", cfg.Timeout)
	}

	write(t, filepath.Join(root, ".romp", "local.toml"), "timeout = \"2h45m\"\n")
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load local timeout: %v", err)
	}
	if cfg.Timeout != "2h45m" {
		t.Errorf("Timeout = %q, want 2h45m from local config", cfg.Timeout)
	}
}

func TestLoadPrecedence(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	write(t, filepath.Join(user, "romp", "config.toml"), "label = \"from-user\"\nwidth = 1\n")

	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "label = \"from-romp\"\n[verify]\ncommands = [\"go test ./...\"]\n")
	write(t, filepath.Join(root, ".romp", "local.toml"), "[harness]\nmodel = \"opus\"\neffort = \"max\"\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Label != "from-romp" {
		t.Errorf("Label = %q, want from-romp (romp.toml beats user)", cfg.Label)
	}
	if cfg.Width != 1 {
		t.Errorf("Width = %d, want 1 (from user, not overridden by romp.toml)", cfg.Width)
	}
	if len(cfg.Verify.Commands) != 1 || cfg.Verify.Commands[0] != "go test ./..." {
		t.Errorf("Verify.Commands = %q, want go test ./...", cfg.Verify.Commands)
	}
	if cfg.Harness.Model != "opus" {
		t.Errorf("Harness.Model = %q, want opus (local.toml)", cfg.Harness.Model)
	}
	if cfg.Harness.Effort != "max" {
		t.Errorf("Harness.Effort = %q, want max (local.toml)", cfg.Harness.Effort)
	}
	if cfg.HarnessEffortSource != filepath.Join(root, ".romp", "local.toml") {
		t.Errorf("HarnessEffortSource = %q, want local.toml", cfg.HarnessEffortSource)
	}
	if cfg.Harness.Default != "codex" {
		t.Errorf("Harness.Default = %q, want codex (default)", cfg.Harness.Default)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "width = 2\n[harness]\nmodel = \"sonnet\"\neffort = \"max\"\n")

	cfg, err := Load(root, Overrides{Width: 9, Model: "opus-flag", Effort: "low"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Width != 9 {
		t.Errorf("Width = %d, want 9 (flag wins)", cfg.Width)
	}
	if cfg.Harness.Model != "opus-flag" {
		t.Errorf("Harness.Model = %q, want opus-flag (flag wins)", cfg.Harness.Model)
	}
	if cfg.Harness.Effort != "low" {
		t.Errorf("Harness.Effort = %q, want low (flag wins)", cfg.Harness.Effort)
	}
	if cfg.HarnessEffortSource != "" {
		t.Errorf("HarnessEffortSource = %q, want empty for flag override", cfg.HarnessEffortSource)
	}
}

func TestLoadEffortSourcePrecedence(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	userConfig := filepath.Join(user, "romp", "config.toml")
	write(t, userConfig, "[harness]\neffort = \"low\"\n")

	root := t.TempDir()
	rompConfig := filepath.Join(root, "romp.toml")
	write(t, rompConfig, "[harness]\neffort = \"medium\"\n")
	localConfig := filepath.Join(root, ".romp", "local.toml")
	write(t, localConfig, "[harness]\neffort = \"high\"\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HarnessEffortSource != localConfig {
		t.Fatalf("HarnessEffortSource = %q, want local config", cfg.HarnessEffortSource)
	}

	if err := os.Remove(localConfig); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load without local effort: %v", err)
	}
	if cfg.HarnessEffortSource != rompConfig {
		t.Fatalf("HarnessEffortSource = %q, want repo config", cfg.HarnessEffortSource)
	}

	if err := os.Remove(rompConfig); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load with global effort: %v", err)
	}
	if cfg.HarnessEffortSource != userConfig {
		t.Fatalf("HarnessEffortSource = %q, want global config", cfg.HarnessEffortSource)
	}
}

func TestLoadOmitsEffortOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "width = 2\n[harness]\nmodel = \"sonnet\"\n")

	cfg, err := Load(root, Overrides{Width: 9, Model: "opus-flag"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Harness.Effort != "high" {
		t.Errorf("Harness.Effort = %q, want high (default survives an unset override)", cfg.Harness.Effort)
	}
}

func TestLoadRejectsUnknownHarness(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "[harness]\ndefault = \"bogus\"\n")
	_, err := Load(root, Overrides{})
	if err == nil {
		t.Fatal("Load() = nil error, want unknown harness")
	}
	want := `unknown harness "bogus" (want claude, codex, or opencode)`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestLoadEffortByHarness(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		o       Overrides
		want    string
		wantErr string
	}{
		{
			name: "shared high on claude",
			toml: "[harness]\ndefault = \"claude\"\neffort = \"high\"\n",
			want: "high",
		},
		{
			name: "shared high on codex",
			toml: "[harness]\ndefault = \"codex\"\neffort = \"high\"\n",
			want: "high",
		},
		{
			name: "OpenCode accepts model-specific variant",
			toml: "[harness]\ndefault = \"opencode\"\neffort = \"experimental\"\n",
			want: "experimental",
		},
		{
			name: "codex-only ultra",
			toml: "[harness]\ndefault = \"codex\"\neffort = \"ultra\"\n",
			want: "ultra",
		},
		{
			name:    "ultra rejected on claude",
			toml:    "[harness]\ndefault = \"claude\"\neffort = \"ultra\"\n",
			wantErr: `harness.effort "ultra" is not valid for claude (want low, medium, high, xhigh, max)`,
		},
		{
			name:    "auto is not a live effort value",
			toml:    "[harness]\neffort = \"auto\"\n",
			wantErr: `harness.effort "auto" is not valid for codex (want none, minimal, low, medium, high, xhigh, max, ultra)`,
		},
		{
			name:    "none rejected on claude",
			toml:    "[harness]\ndefault = \"claude\"\neffort = \"none\"\n",
			wantErr: `harness.effort "none" is not valid for claude (want low, medium, high, xhigh, max)`,
		},
		{
			name:    "unknown effort",
			toml:    "[harness]\ndefault = \"codex\"\neffort = \"ludicrous\"\n",
			wantErr: `harness.effort "ludicrous" is not valid for codex (want none, minimal, low, medium, high, xhigh, max, ultra)`,
		},
		{
			name:    "flag switches onto an incompatible effort",
			toml:    "[harness]\ndefault = \"codex\"\neffort = \"ultra\"\n",
			o:       Overrides{Harness: "claude"},
			wantErr: `harness.effort "ultra" is not valid for claude (want low, medium, high, xhigh, max)`,
		},
		{
			name: "flag switches onto a shared effort",
			toml: "[harness]\ndefault = \"claude\"\neffort = \"max\"\n",
			o:    Overrides{Harness: "codex"},
			want: "max",
		},
		{
			name:    "flag effort is rejected by selected harness",
			toml:    "[harness]\ndefault = \"claude\"\neffort = \"medium\"\n",
			o:       Overrides{Effort: "ultra"},
			wantErr: `harness.effort "ultra" is not valid for claude (want low, medium, high, xhigh, max)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			root := t.TempDir()
			write(t, filepath.Join(root, "romp.toml"), tt.toml)
			cfg, err := Load(root, tt.o)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() = %+v, nil error, want %q", cfg, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("err = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Harness.Effort != tt.want {
				t.Errorf("Effort = %q, want %q", cfg.Harness.Effort, tt.want)
			}
		})
	}
}

func TestLoadLabelOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "claimed_label = \"team-claimed\"\nblocked_label = \"team-blocked\"\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClaimedLabel != "team-claimed" {
		t.Errorf("ClaimedLabel = %q, want team-claimed", cfg.ClaimedLabel)
	}
	if cfg.BlockedLabel != "team-blocked" {
		t.Errorf("BlockedLabel = %q, want team-blocked", cfg.BlockedLabel)
	}
}

func TestHistoryDaysGlobalOnly(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	write(t, filepath.Join(user, "romp", "config.toml"), "history_days = 7\n")

	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "history_days = 90\n")
	write(t, filepath.Join(root, ".romp", "local.toml"), "history_days = 5\n")

	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HistoryDays != 7 {
		t.Errorf("HistoryDays = %d, want 7 (user config only)", cfg.HistoryDays)
	}
}

func TestHistoryDaysDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "history_days = 90\n")
	cfg, err := Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HistoryDays != 30 {
		t.Errorf("HistoryDays = %d, want 30 (romp.toml ignored)", cfg.HistoryDays)
	}
}

func TestLoadMalformed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "label = [not a string\n")
	if _, err := Load(root, Overrides{}); err == nil {
		t.Fatal("Load() = nil error, want parse error")
	}
}

func TestDiscoverCollectsStructuredProjectFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Makefile"), "test lint:\n\t@true\n")
	write(t, filepath.Join(root, "package.json"), `{"scripts":{"check":"go test ./..."}}`)
	write(t, filepath.Join(root, "go.mod"), "module example\n")
	write(t, filepath.Join(root, ".github", "workflows", "test.yml"), "jobs:\n  test:\n    steps:\n      - run: docker build .\n")
	write(t, filepath.Join(root, "README.md"), "```sh\nmake test\n```\n")

	got := Discover(root)
	if len(got) != 4 {
		t.Fatalf("Discover = %+v, want four candidates", got)
	}
	for _, candidate := range got {
		if candidate.Command == "make test" {
			if len(candidate.Sources) != 1 {
				t.Errorf("Discover included Markdown candidate: %+v", candidate)
			}
		}
	}
	if got[0].Command != "make test" || got[1].Command != "make lint" || got[2].Command != "npm run check" || got[3].Command != "go test ./..." {
		t.Errorf("Discover order = %+v", got)
	}
}

func TestDiscoverIncludesAllMakeTargets(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Makefile"), "dragon:\n\t@true\n")
	got := Discover(root)
	if len(got) != 1 || got[0].Command != "make dragon" {
		t.Errorf("Discover = %+v, want make dragon", got)
	}
}

func TestDiscoverCollectsNestedPackageScriptsAndExcludesCI(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "frontend", "package.json"), `{"scripts":{"check":"echo check"}}`)
	write(t, filepath.Join(root, ".github", "workflows", "test.yml"), "jobs:\n  test:\n    steps:\n      - run: |\n          make test\n          make lint\n")

	got := Discover(root)
	want := []string{"npm --prefix frontend run check"}
	if len(got) != len(want) {
		t.Fatalf("Discover = %+v, want %d candidates", got, len(want))
	}
	for i, candidate := range got {
		if candidate.Command != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, candidate.Command, want[i])
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
