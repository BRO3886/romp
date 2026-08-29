package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type remoteFixture struct {
	repo      *Repo
	remote    string
	publisher string
	initial   string
}

func newRemoteFixture(t *testing.T, defaultBranch string) remoteFixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	publisher := filepath.Join(root, "publisher")
	operator := filepath.Join(root, "operator")

	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "init", "-b", defaultBranch, publisher)
	runGit(t, publisher, "config", "user.name", "Romp Test")
	runGit(t, publisher, "config", "user.email", "romp@example.com")
	if err := os.WriteFile(filepath.Join(publisher, "marker.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, publisher, "add", "marker.txt")
	runGit(t, publisher, "commit", "-m", "first")
	runGit(t, publisher, "remote", "add", "origin", remote)
	runGit(t, publisher, "push", "-u", "origin", defaultBranch)
	runGit(t, root, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/"+defaultBranch)
	runGit(t, root, "clone", remote, operator)

	return remoteFixture{
		repo:      &Repo{Root: operator},
		remote:    remote,
		publisher: publisher,
		initial:   gitOutput(t, operator, "rev-parse", "HEAD"),
	}
}

func (f remoteFixture) commitAndPush(t *testing.T, branch, marker string) string {
	t.Helper()
	runGit(t, f.publisher, "checkout", branch)
	if err := os.WriteFile(filepath.Join(f.publisher, "marker.txt"), []byte(marker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, f.publisher, "add", "marker.txt")
	runGit(t, f.publisher, "commit", "-m", marker)
	runGit(t, f.publisher, "push", "origin", branch)
	return gitOutput(t, f.publisher, "rev-parse", "HEAD")
}

type jobStart struct {
	base   string
	commit string
	marker string
}

func startTestJob(t *testing.T, repo *Repo, issue int, configuredBase string) jobStart {
	t.Helper()
	base, commit, err := repo.SyncBase(context.Background(), configuredBase)
	if err != nil {
		t.Fatalf("SyncBase: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "worktree")
	if err := repo.AddWorktree(context.Background(), "romp-test-"+strconv.Itoa(issue), dir, commit); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	t.Cleanup(func() { _ = repo.RemoveWorktree(context.Background(), dir) })
	return jobStart{
		base:   base,
		commit: gitOutput(t, dir, "rev-parse", "HEAD"),
		marker: strings.TrimSpace(string(mustReadFile(t, filepath.Join(dir, "marker.txt")))),
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSecondJobStartsFromCommitPushedAfterFirstJob(t *testing.T) {
	f := newRemoteFixture(t, "trunk")

	first := startTestJob(t, f.repo, 1, "")
	if first.base != "trunk" || first.commit != f.initial || first.marker != "first" {
		t.Fatalf("first job = %+v, want trunk at %s with first", first, f.initial)
	}

	secondCommit := f.commitAndPush(t, "trunk", "second")
	second := startTestJob(t, f.repo, 2, "")
	if second.base != "trunk" || second.commit != secondCommit || second.marker != "second" {
		t.Fatalf("second job = %+v, want trunk at %s with second", second, secondCommit)
	}
	if got := gitOutput(t, f.repo.Root, "rev-parse", "HEAD"); got != f.initial {
		t.Errorf("operator HEAD = %s, want unchanged %s", got, f.initial)
	}
	if got := gitOutput(t, f.repo.Root, "status", "--porcelain"); got != "" {
		t.Errorf("operator worktree changed:\n%s", got)
	}
	if got := strings.TrimSpace(string(mustReadFile(t, filepath.Join(f.repo.Root, "marker.txt")))); got != "first" {
		t.Errorf("operator marker = %q, want first", got)
	}
}

func TestNextJobUsesChangedRemoteDefaultBranch(t *testing.T) {
	f := newRemoteFixture(t, "trunk")
	runGit(t, f.publisher, "checkout", "-b", "stable")
	stableCommit := f.commitAndPush(t, "stable", "stable")
	runGit(t, filepath.Dir(f.remote), "--git-dir", f.remote, "symbolic-ref", "HEAD", "refs/heads/stable")

	job := startTestJob(t, f.repo, 3, "")
	if job.base != "stable" || job.commit != stableCommit || job.marker != "stable" {
		t.Fatalf("job = %+v, want stable at %s with stable", job, stableCommit)
	}
}

func TestConfiguredBaseOverridesRemoteDefaultAtLatestCommit(t *testing.T) {
	f := newRemoteFixture(t, "trunk")
	runGit(t, f.publisher, "checkout", "-b", "stable")
	f.commitAndPush(t, "stable", "stable")
	runGit(t, filepath.Dir(f.remote), "--git-dir", f.remote, "symbolic-ref", "HEAD", "refs/heads/stable")
	trunkCommit := f.commitAndPush(t, "trunk", "latest-trunk")

	job := startTestJob(t, f.repo, 4, "trunk")
	if job.base != "trunk" || job.commit != trunkCommit || job.marker != "latest-trunk" {
		t.Fatalf("job = %+v, want trunk at %s with latest-trunk", job, trunkCommit)
	}
}

func TestSyncBaseReportsDefaultBranchResolutionFailure(t *testing.T) {
	f := newRemoteFixture(t, "trunk")
	runGit(t, filepath.Dir(f.remote), "--git-dir", f.remote, "symbolic-ref", "HEAD", "refs/heads/missing")

	_, _, err := f.repo.SyncBase(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "remote HEAD does not identify a default branch") || !strings.Contains(err.Error(), "configure base explicitly") {
		t.Fatalf("SyncBase error = %v, want actionable default-branch error", err)
	}
}

func TestSyncBaseReportsFetchFailure(t *testing.T) {
	f := newRemoteFixture(t, "trunk")

	_, _, err := f.repo.SyncBase(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "refreshing origin/missing") {
		t.Fatalf("SyncBase error = %v, want actionable fetch error", err)
	}
}

func TestConcurrentBaseRefreshesSucceed(t *testing.T) {
	f := newRemoteFixture(t, "trunk")
	const jobs = 12
	errCh := make(chan string, jobs)
	var wg sync.WaitGroup
	wg.Add(jobs)
	for range jobs {
		go func() {
			defer wg.Done()
			base, commit, err := f.repo.SyncBase(context.Background(), "")
			if err != nil {
				errCh <- err.Error()
				return
			}
			if base != "trunk" || commit != f.initial {
				errCh <- "got base " + base + " at " + commit
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent SyncBase: %v", err)
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
