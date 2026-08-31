package gitfixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type Fixture struct {
	Remote    string
	Publisher string
	Operator  string
	Initial   string
}

func New(t testing.TB, defaultBranch string) Fixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	publisher := filepath.Join(root, "publisher")
	operator := filepath.Join(root, "operator")

	Run(t, root, "init", "--bare", remote)
	Run(t, root, "init", "-b", defaultBranch, publisher)
	Run(t, publisher, "config", "user.name", "Romp Test")
	Run(t, publisher, "config", "user.email", "romp@example.com")
	writeMarker(t, publisher, "first")
	Run(t, publisher, "add", "marker.txt")
	Run(t, publisher, "commit", "-m", "first")
	Run(t, publisher, "remote", "add", "origin", remote)
	Run(t, publisher, "push", "-u", "origin", defaultBranch)
	Run(t, root, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/"+defaultBranch)
	Run(t, root, "clone", remote, operator)

	return Fixture{
		Remote:    remote,
		Publisher: publisher,
		Operator:  operator,
		Initial:   Output(t, operator, "rev-parse", "HEAD"),
	}
}

func (f Fixture) CommitAndPush(t testing.TB, branch, marker string) string {
	t.Helper()
	Run(t, f.Publisher, "checkout", branch)
	writeMarker(t, f.Publisher, marker)
	Run(t, f.Publisher, "add", "marker.txt")
	Run(t, f.Publisher, "commit", "-m", marker)
	Run(t, f.Publisher, "push", "origin", branch)
	return Output(t, f.Publisher, "rev-parse", "HEAD")
}

func Run(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func Output(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeMarker(t testing.TB, dir, marker string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte(marker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
