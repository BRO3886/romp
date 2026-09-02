package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func commandSkillFS() fstest.MapFS {
	return fstest.MapFS{
		"skills/rompify/SKILL.md": {
			Data: []byte("---\nname: rompify\n---\n"),
		},
		"skills/rompify/references/contract.md": {
			Data: []byte("# Contract\n"),
		},
	}
}

func TestSkillsInstallForExplicitAgent(t *testing.T) {
	homeDir := t.TempDir()
	cmd := newSkillsCmd(commandSkillFS(), "v0.2.0", func() (string, error) { return homeDir, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"install", "--agent", "codex"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills install: %v", err)
	}
	installed := filepath.Join(homeDir, ".agents", "skills", "rompify", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed skill: %v", err)
	}
	if !strings.Contains(out.String(), "Installed rompify skill to ~/.agents/skills/rompify") {
		t.Errorf("output = %q", out.String())
	}
}

func TestSkillsInstallDetectsExistingAgent(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".claude", "skills"), 0o755); err != nil {
		t.Fatalf("create detected agent: %v", err)
	}
	cmd := newSkillsCmd(commandSkillFS(), "dev", func() (string, error) { return homeDir, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"install"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills install: %v", err)
	}
	installed := filepath.Join(homeDir, ".claude", "skills", "rompify", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed detected skill: %v", err)
	}
}

func TestSkillsInstallWithoutDetectedAgentFailsClosed(t *testing.T) {
	homeDir := t.TempDir()
	cmd := newSkillsCmd(commandSkillFS(), "dev", func() (string, error) { return homeDir, nil })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"install"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no supported agent directories detected") {
		t.Fatalf("skills install error = %v", err)
	}
}

func TestSkillsInstallDryRunDoesNotWrite(t *testing.T) {
	homeDir := t.TempDir()
	cmd := newSkillsCmd(commandSkillFS(), "dev", func() (string, error) { return homeDir, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"install", "--agent", "codex", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills install --dry-run: %v", err)
	}
	installed := filepath.Join(homeDir, ".agents", "skills", "rompify")
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("dry run created %s", installed)
	}
	for _, want := range []string{"SKILL.md", "references/contract.md", ".romp-version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry-run output missing %q: %q", want, out.String())
		}
	}
}

func TestSkillsStatusReportsInstalledVersion(t *testing.T) {
	homeDir := t.TempDir()
	install := newSkillsCmd(commandSkillFS(), "v0.2.0", func() (string, error) { return homeDir, nil })
	install.SetOut(&bytes.Buffer{})
	install.SetArgs([]string{"install", "--agent", "codex"})
	if err := install.Execute(); err != nil {
		t.Fatalf("skills install: %v", err)
	}

	status := newSkillsCmd(commandSkillFS(), "v0.2.0", func() (string, error) { return homeDir, nil })
	var out bytes.Buffer
	status.SetOut(&out)
	status.SetArgs([]string{"status"})
	if err := status.Execute(); err != nil {
		t.Fatalf("skills status: %v", err)
	}
	if !strings.Contains(out.String(), "Codex CLI") || !strings.Contains(out.String(), "installed v0.2.0") {
		t.Errorf("status output = %q", out.String())
	}
}

func TestSkillsUninstallRemovesExplicitAgentSkill(t *testing.T) {
	homeDir := t.TempDir()
	install := newSkillsCmd(commandSkillFS(), "dev", func() (string, error) { return homeDir, nil })
	install.SetOut(&bytes.Buffer{})
	install.SetArgs([]string{"install", "--agent", "codex"})
	if err := install.Execute(); err != nil {
		t.Fatalf("skills install: %v", err)
	}

	uninstall := newSkillsCmd(commandSkillFS(), "dev", func() (string, error) { return homeDir, nil })
	var out bytes.Buffer
	uninstall.SetOut(&out)
	uninstall.SetArgs([]string{"uninstall", "--agent", "codex"})
	if err := uninstall.Execute(); err != nil {
		t.Fatalf("skills uninstall: %v", err)
	}
	installed := filepath.Join(homeDir, ".agents", "skills", "rompify")
	if _, err := os.Lstat(installed); !os.IsNotExist(err) {
		t.Fatalf("uninstall left %s", installed)
	}
	if !strings.Contains(out.String(), "Removed rompify skill from ~/.agents/skills/rompify") {
		t.Errorf("output = %q", out.String())
	}
}

func TestRootIncludesSkillsCommand(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"skills"})
	if err != nil {
		t.Fatalf("find skills command: %v", err)
	}
	if cmd.Name() != "skills" {
		t.Fatalf("root command resolved %q, want skills", cmd.Name())
	}
}
