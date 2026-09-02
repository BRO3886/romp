package skills

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func testSkillFS() fstest.MapFS {
	return fstest.MapFS{
		"skills/rompify/SKILL.md": {
			Data: []byte("---\nname: rompify\n---\n"),
		},
		"skills/rompify/references/contract.md": {
			Data: []byte("# Contract\n"),
		},
	}
}

func TestDefaultTargets(t *testing.T) {
	targets := DefaultTargets("/home/sid")
	want := []AgentTarget{
		{Name: "Claude Code", Key: "claude", BaseDir: "/home/sid/.claude/skills"},
		{Name: "Codex CLI", Key: "codex", BaseDir: "/home/sid/.agents/skills"},
		{Name: "OpenClaw", Key: "openclaw", BaseDir: "/home/sid/.openclaw/skills"},
	}
	if len(targets) != len(want) {
		t.Fatalf("DefaultTargets() returned %d targets, want %d", len(targets), len(want))
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Errorf("DefaultTargets()[%d] = %#v, want %#v", i, targets[i], want[i])
		}
	}
}

func TestInstallWritesEmbeddedSkillAndVersion(t *testing.T) {
	target := AgentTarget{BaseDir: filepath.Join(t.TempDir(), "skills")}

	written, err := Install(testSkillFS(), target, "v0.2.0")
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	wantWritten := []string{"SKILL.md", "references/contract.md"}
	if len(written) != len(wantWritten) {
		t.Fatalf("Install() wrote %v, want %v", written, wantWritten)
	}
	for i := range wantWritten {
		if written[i] != wantWritten[i] {
			t.Errorf("Install() file %d = %q, want %q", i, written[i], wantWritten[i])
		}
	}

	skillBody, err := os.ReadFile(filepath.Join(SkillDir(target), "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed SKILL.md: %v", err)
	}
	if got, want := string(skillBody), "---\nname: rompify\n---\n"; got != want {
		t.Errorf("installed SKILL.md = %q, want %q", got, want)
	}
	if got := InstalledVersion(target); got != "v0.2.0" {
		t.Errorf("InstalledVersion() = %q, want %q", got, "v0.2.0")
	}
}

func TestInstallReplacesExistingSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := AgentTarget{BaseDir: filepath.Join(tempDir, "skills")}
	destination := SkillDir(target)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create target parent: %v", err)
	}
	linkTarget := filepath.Join(tempDir, "old-skill")
	if err := os.MkdirAll(linkTarget, 0o755); err != nil {
		t.Fatalf("create link target: %v", err)
	}
	if err := os.Symlink(linkTarget, destination); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if _, err := Install(testSkillFS(), target, "dev"); err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatalf("stat installed skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("installed skill directory remains a symlink")
	}
}

func TestInstallKeepsExistingSkillWhenBundleIsInvalid(t *testing.T) {
	target := AgentTarget{BaseDir: filepath.Join(t.TempDir(), "skills")}
	if _, err := Install(testSkillFS(), target, "v0.1.0"); err != nil {
		t.Fatalf("initial Install() error: %v", err)
	}

	invalidBundle := fstest.MapFS{
		"skills/rompify/references/contract.md": {Data: []byte("# Contract\n")},
	}
	if _, err := Install(invalidBundle, target, "v0.2.0"); err == nil {
		t.Fatal("Install() error = nil for bundle without SKILL.md")
	}
	if !IsInstalled(target) {
		t.Fatal("invalid update removed the existing skill")
	}
	if got := InstalledVersion(target); got != "v0.1.0" {
		t.Errorf("InstalledVersion() = %q, want existing version %q", got, "v0.1.0")
	}
}

func TestUninstall(t *testing.T) {
	target := AgentTarget{BaseDir: filepath.Join(t.TempDir(), "skills")}
	if _, err := Install(testSkillFS(), target, "dev"); err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	removed, err := Uninstall(target)
	if err != nil {
		t.Fatalf("Uninstall() error: %v", err)
	}
	if !removed {
		t.Fatal("Uninstall() removed = false, want true")
	}
	removed, err = Uninstall(target)
	if err != nil {
		t.Fatalf("second Uninstall() error: %v", err)
	}
	if removed {
		t.Fatal("second Uninstall() removed = true, want false")
	}
}

func TestDetectAgents(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents", "skills"), 0o755); err != nil {
		t.Fatalf("create Codex skill root: %v", err)
	}

	detected := DetectAgents(DefaultTargets(homeDir))
	if len(detected) != 1 || detected[0].Key != "codex" {
		t.Fatalf("DetectAgents() = %#v, want Codex only", detected)
	}
}

func TestDisplayPathDoesNotCollapseSimilarPrefix(t *testing.T) {
	if got, want := DisplayPath("/home/sidebar/skill", "/home/sid"), "/home/sidebar/skill"; got != want {
		t.Errorf("DisplayPath() = %q, want %q", got, want)
	}
	if got, want := DisplayPath("/home/sid/.agents/skills/rompify", "/home/sid"), "~/.agents/skills/rompify"; got != want {
		t.Errorf("DisplayPath() = %q, want %q", got, want)
	}
}
