package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/config"
)

func TestGitVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		minMaj  int
		minMin  int
		want    bool
	}{
		{"git version 2.50.1", 2, 35, true},
		{"git version 2.50.1 (Apple Git-155)", 2, 35, true},
		{"git version 2.35.0", 2, 35, true},
		{"git version 2.34.9", 2, 35, false},
		{"git version 2.35", 2, 35, true},
		{"git version 3.0.0", 2, 35, true},
		{"git version 1.9.0", 2, 35, false},
		{"git version 2.4", 2, 35, false},
		{"garbage", 2, 35, false},
		{"", 2, 35, false},
	}
	for _, tt := range tests {
		if got := gitVersionAtLeast(tt.version, tt.minMaj, tt.minMin); got != tt.want {
			t.Errorf("gitVersionAtLeast(%q, %d, %d) = %v, want %v", tt.version, tt.minMaj, tt.minMin, got, tt.want)
		}
	}
}

func TestCheckReviewHarnessUsesConfiguredCrossHarness(t *testing.T) {
	bin := t.TempDir()
	writeDoctorCLI(t, bin, "claude", "claude 2.1.0")
	writeDoctorCLI(t, bin, "codex", "codex-cli 0.146.0")
	t.Setenv("PATH", bin)

	cfg := config.Defaults()
	cfg.Harness.Default = "codex"
	cfg.Review.Harness = "claude"
	detail, err := checkReviewHarness(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("checkReviewHarness: %v", err)
	}
	if detail != "claude claude 2.1.0" {
		t.Errorf("detail = %q, want claude claude 2.1.0", detail)
	}
}

func TestCheckReviewHarnessMissingCLIIsActionable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := config.Defaults()
	cfg.Review.Harness = "claude"

	_, err := checkReviewHarness(context.Background(), &cfg)
	if err == nil {
		t.Fatal("checkReviewHarness = nil error, want missing CLI")
	}
	for _, want := range []string{"review harness claude is unavailable", "claude CLI not found on PATH", "install Claude Code and log in"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func TestCheckReviewHarnessDisabled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := config.Defaults()
	cfg.Review.Enabled = false
	detail, err := checkReviewHarness(context.Background(), &cfg)
	if err != nil || detail != "disabled" {
		t.Errorf("checkReviewHarness = %q, %v; want disabled, nil", detail, err)
	}
}

func writeDoctorCLI(t *testing.T, dir, name, version string) {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + version + "'; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
