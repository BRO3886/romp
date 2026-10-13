package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallerVersionOrdering(t *testing.T) {
	for _, test := range []struct {
		old, new string
		allowed  bool
	}{{"v0.1.0-alpha.6", "v0.1.0-alpha.7", true}, {"v0.1.0-alpha.7", "v0.1.0-alpha.6", false}, {"v0.1.0-alpha.7", "v0.1.0", true}, {"v0.1.0", "v0.1.0-alpha.7", false}, {"v1.2.0", "v1.1.9", false}, {"v1.2.0", "v1.3.0", true}, {"dev", "dev", true}, {"v1.0.0", "dev", false}} {
		if err := allowUpgrade(test.old, test.new); (err == nil) != test.allowed {
			t.Errorf("upgrade %s -> %s: %v", test.old, test.new, err)
		}
	}
}

func TestPlistMatchesLaunchdSchema(t *testing.T) {
	paths := InstallPaths{Binary: "/tmp/Application Support/a & b/rompd", Environment: "/tmp/a & b/environment.json"}
	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, []byte(agentPlist(paths, Environment{Path: "/usr/bin:/bin"})), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %s %v", out, err)
	}
	out, err := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var plist map[string]any
	if err := json.Unmarshal(out, &plist); err != nil {
		t.Fatal(err)
	}
	if plist["Program"] != paths.Binary || plist["KeepAlive"] != true || plist["RunAtLoad"] != true {
		t.Fatalf("launchd contract=%+v", plist)
	}
}

func TestMovedProgramFailsDaemonEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	env := Environment{Path: dir, Programs: map[string]string{"git": path}}
	if err := validateEnvironment(env); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := validateEnvironment(env); err == nil {
		t.Fatal("stale pinned path accepted")
	}
}

func TestInstallPreflightFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(context.Background(), dir, filepath.Join(dir, "missing-rompd")); err == nil {
		t.Fatal("missing binary accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("preflight provisioned files: %v %v", entries, err)
	}
}
