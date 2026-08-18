package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Label != "romp" || cfg.Width != 3 || cfg.Timeout != "25m" || cfg.Harness.Default != "claude" {
		t.Fatalf("Defaults() = %+v, want label=romp width=3 timeout=25m harness.default=claude", cfg)
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
	write(t, filepath.Join(root, ".romp", "local.toml"), "[harness]\nmodel = \"opus\"\n")

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
	if cfg.Harness.Default != "claude" {
		t.Errorf("Harness.Default = %q, want claude (default)", cfg.Harness.Default)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "romp.toml"), "width = 2\n[harness]\nmodel = \"sonnet\"\n")

	cfg, err := Load(root, Overrides{Width: 9, Model: "opus-flag"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Width != 9 {
		t.Errorf("Width = %d, want 9 (flag wins)", cfg.Width)
	}
	if cfg.Harness.Model != "opus-flag" {
		t.Errorf("Harness.Model = %q, want opus-flag (flag wins)", cfg.Harness.Model)
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
