package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(context.Background(), sub)
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FindRoot = %q, want %q", got, want)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		owner, repo string
		wantErr     bool
	}{
		{"scp", "git@github.com:BRO3886/romp.git", "BRO3886", "romp", false},
		{"https", "https://github.com/BRO3886/romp.git", "BRO3886", "romp", false},
		{"https no git suffix", "https://github.com/BRO3886/romp", "BRO3886", "romp", false},
		{"scp no git suffix", "git@github.com:BRO3886/romp", "BRO3886", "romp", false},
		{"trailing slash", "https://github.com/owner/name/", "owner", "name", false},
		{"not a remote", "not-a-remote", "", "", true},
		{"empty", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRemote(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRemote(%q) error = %v, want error: %v", tt.url, err, tt.wantErr)
			}
			if owner != tt.owner || repo != tt.repo {
				t.Errorf("parseRemote(%q) = %q/%q, want %q/%q", tt.url, owner, repo, tt.owner, tt.repo)
			}
		})
	}
}
