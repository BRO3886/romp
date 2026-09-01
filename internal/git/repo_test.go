package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/BRO3886/romp/internal/testutil/gitfixture"
)

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	gitfixture.Run(t, root, "init")
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

type jobStart struct {
	base   string
	commit string
	marker string
}

func startTestJob(t *testing.T, repo *Repo, issue int, configuredBase string) jobStart {
	t.Helper()
	base := configuredBase
	if base == "" {
		var err error
		base, err = repo.DefaultBranch(context.Background())
		if err != nil {
			t.Fatalf("DefaultBranch: %v", err)
		}
	}
	commit, err := repo.RefreshBranch(context.Background(), base)
	if err != nil {
		t.Fatalf("RefreshBranch: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "worktree")
	if err := repo.AddWorktree(context.Background(), "romp-test-"+strconv.Itoa(issue), dir, commit); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	t.Cleanup(func() { _ = repo.RemoveWorktree(context.Background(), dir) })
	return jobStart{
		base:   base,
		commit: gitfixture.Output(t, dir, "rev-parse", "HEAD"),
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
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}

	first := startTestJob(t, repo, 1, "")
	if first.base != "trunk" || first.commit != fixture.Initial || first.marker != "first" {
		t.Fatalf("first job = %+v, want trunk at %s with first", first, fixture.Initial)
	}

	secondCommit := fixture.CommitAndPush(t, "trunk", "second")
	second := startTestJob(t, repo, 2, "")
	if second.base != "trunk" || second.commit != secondCommit || second.marker != "second" {
		t.Fatalf("second job = %+v, want trunk at %s with second", second, secondCommit)
	}
	if got := gitfixture.Output(t, repo.Root, "rev-parse", "HEAD"); got != fixture.Initial {
		t.Errorf("operator HEAD = %s, want unchanged %s", got, fixture.Initial)
	}
	if got := gitfixture.Output(t, repo.Root, "status", "--porcelain"); got != "" {
		t.Errorf("operator worktree changed:\n%s", got)
	}
	if got := strings.TrimSpace(string(mustReadFile(t, filepath.Join(repo.Root, "marker.txt")))); got != "first" {
		t.Errorf("operator marker = %q, want first", got)
	}
}

func TestNextJobUsesChangedRemoteDefaultBranch(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}
	gitfixture.Run(t, fixture.Publisher, "checkout", "-b", "stable")
	stableCommit := fixture.CommitAndPush(t, "stable", "stable")
	gitfixture.Run(t, filepath.Dir(fixture.Remote), "--git-dir", fixture.Remote, "symbolic-ref", "HEAD", "refs/heads/stable")

	job := startTestJob(t, repo, 3, "")
	if job.base != "stable" || job.commit != stableCommit || job.marker != "stable" {
		t.Fatalf("job = %+v, want stable at %s with stable", job, stableCommit)
	}
}

func TestConfiguredBaseOverridesRemoteDefaultAtLatestCommit(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}
	gitfixture.Run(t, fixture.Publisher, "checkout", "-b", "stable")
	fixture.CommitAndPush(t, "stable", "stable")
	gitfixture.Run(t, filepath.Dir(fixture.Remote), "--git-dir", fixture.Remote, "symbolic-ref", "HEAD", "refs/heads/stable")
	trunkCommit := fixture.CommitAndPush(t, "trunk", "latest-trunk")

	job := startTestJob(t, repo, 4, "trunk")
	if job.base != "trunk" || job.commit != trunkCommit || job.marker != "latest-trunk" {
		t.Fatalf("job = %+v, want trunk at %s with latest-trunk", job, trunkCommit)
	}
}

func TestDefaultBranchReportsRemoteResolutionFailure(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}
	gitfixture.Run(t, filepath.Dir(fixture.Remote), "--git-dir", fixture.Remote, "symbolic-ref", "HEAD", "refs/heads/missing")

	_, err := repo.DefaultBranch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "remote HEAD does not identify a default branch") || !strings.Contains(err.Error(), "configure base explicitly") {
		t.Fatalf("DefaultBranch error = %v, want actionable default-branch error", err)
	}
}

func TestRefreshBranchReportsFetchFailure(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}

	_, err := repo.RefreshBranch(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "refreshing origin/missing") {
		t.Fatalf("RefreshBranch error = %v, want actionable fetch error", err)
	}
}

func TestConcurrentBranchRefreshesSucceed(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}
	const jobs = 12
	errCh := make(chan string, jobs)
	var wg sync.WaitGroup
	wg.Add(jobs)
	for range jobs {
		go func() {
			defer wg.Done()
			commit, err := repo.RefreshBranch(context.Background(), "trunk")
			if err != nil {
				errCh <- err.Error()
				return
			}
			if commit != fixture.Initial {
				errCh <- "got commit " + commit
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent RefreshBranch: %v", err)
	}
}

func TestReviewInputsDescribeCommittedBranchAgainstBase(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	repo := &Repo{Root: fixture.Operator}
	dir := filepath.Join(t.TempDir(), "worktree")
	if err := repo.AddWorktree(context.Background(), "romp-review-inputs", dir, fixture.Initial); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	t.Cleanup(func() { _ = repo.RemoveWorktree(context.Background(), dir) })
	if err := os.WriteFile(filepath.Join(dir, "review.go"), []byte("package review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitfixture.Run(t, dir, "add", "review.go")
	gitfixture.Run(t, dir, "commit", "-m", "feat: add review input")

	files, err := repo.ChangedFiles(context.Background(), dir, fixture.Initial)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "review.go" {
		t.Fatalf("ChangedFiles = %v, want [review.go]", files)
	}
	diff, err := repo.Diff(context.Background(), dir, fixture.Initial)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "+package review") {
		t.Errorf("Diff missing committed content:\n%s", diff)
	}
	log, err := repo.BranchLog(context.Background(), dir, fixture.Initial)
	if err != nil {
		t.Fatalf("BranchLog: %v", err)
	}
	if !strings.Contains(log, "feat: add review input") {
		t.Errorf("BranchLog = %q, want commit subject", log)
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
