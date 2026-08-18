package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBlockedMissingReturnsEmpty(t *testing.T) {
	gap, err := readBlocked(t.TempDir())
	if err != nil {
		t.Fatalf("readBlocked: %v", err)
	}
	if gap != "" {
		t.Errorf("gap = %q, want empty", gap)
	}
}

func TestReadBlockedReadsGap(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".romp"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, blockedFile), []byte("  criteria contradict each other\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gap, err := readBlocked(dir)
	if err != nil {
		t.Fatalf("readBlocked: %v", err)
	}
	if gap != "criteria contradict each other" {
		t.Errorf("gap = %q, want %q", gap, "criteria contradict each other")
	}
}

func TestBlockedComment(t *testing.T) {
	c := blockedComment("gap here")
	if !strings.Contains(c, "under-scoped") || !strings.Contains(c, "gap here") {
		t.Errorf("blockedComment = %q", c)
	}
}
