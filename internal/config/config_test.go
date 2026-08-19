package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Label != "romp" || cfg.ClaimedLabel != "romp:claimed" || cfg.BlockedLabel != "romp:blocked" ||
		cfg.Width != 3 || cfg.Timeout != "25m" || cfg.HistoryDays != 30 ||
		cfg.Harness.Default != "codex" || cfg.Harness.Effort != "high" {
		t.Fatalf("Defaults() = %+v", cfg)
	}
}

func TestLoadNoFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load(t.TempDir(), Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != "romp" || cfg.Width != 3 {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
}

func TestLoadPrecedence(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	write(t, filepath.Join(user, "romp", "config.toml"), "label = \"from-user\"\nwidth = 1\n")

	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "label = \"from-romp\"\n[verify]\ntest = \"go test ./...\"\n")
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
	if cfg.Verify.Test != "go test ./..." {
		t.Errorf("Verify.Test = %q, want go test ./...", cfg.Verify.Test)
	}
	if cfg.Harness.Model != "opus" {
		t.Errorf("Harness.Model = %q, want opus (local.toml)", cfg.Harness.Model)
	}
	if cfg.Harness.Effort != "max" {
		t.Errorf("Harness.Effort = %q, want max (local.toml)", cfg.Harness.Effort)
	}
	if cfg.Harness.EffortSource != filepath.Join(root, ".romp", "local.toml") {
		t.Errorf("Harness.EffortSource = %q, want local.toml", cfg.Harness.EffortSource)
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
	if cfg.Harness.EffortSource != "" {
		t.Errorf("Harness.EffortSource = %q, want empty for flag override", cfg.Harness.EffortSource)
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
	if cfg.Harness.EffortSource != localConfig {
		t.Fatalf("EffortSource = %q, want local config", cfg.Harness.EffortSource)
	}

	if err := os.Remove(localConfig); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load without local effort: %v", err)
	}
	if cfg.Harness.EffortSource != rompConfig {
		t.Fatalf("EffortSource = %q, want repo config", cfg.Harness.EffortSource)
	}

	if err := os.Remove(rompConfig); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(root, Overrides{})
	if err != nil {
		t.Fatalf("Load with global effort: %v", err)
	}
	if cfg.Harness.EffortSource != userConfig {
		t.Fatalf("EffortSource = %q, want global config", cfg.Harness.EffortSource)
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

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantBuild string
		wantTest  string
		wantLang  string
	}{
		{"go", "go.mod", "go build ./...", "go test ./... -count=1", "go"},
		{"rust", "Cargo.toml", "cargo build", "cargo test", "rust"},
		{"node", "package.json", "", "npm test", "node"},
		{"python", "pyproject.toml", "", "pytest", "python"},
		{"make", "Makefile", "", "make test", "make"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, tt.file), "")
			build, test, lang := Detect(root)
			if build != tt.wantBuild || test != tt.wantTest || lang != tt.wantLang {
				t.Errorf("Detect = (%q, %q, %q), want (%q, %q, %q)", build, test, lang, tt.wantBuild, tt.wantTest, tt.wantLang)
			}
		})
	}
}

func TestDetectUnknown(t *testing.T) {
	build, test, lang := Detect(t.TempDir())
	if build != "" || test != "" || lang != "" {
		t.Errorf("Detect(empty) = (%q, %q, %q), want empty", build, test, lang)
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
