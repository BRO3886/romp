package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	gitops "github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/runner"
	"github.com/BRO3886/romp/internal/testutil/gitfixture"
)

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "omitted", want: 0},
		{name: "minutes", value: "25m", want: 25 * time.Minute},
		{name: "fractional hours", value: "1.5h", want: 90 * time.Minute},
		{name: "composite", value: "2h45m", want: 2*time.Hour + 45*time.Minute},
		{name: "invalid", value: "later", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimeout(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseTimeout(%q) error = %v, want error: %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseTimeout(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestBuildReviewHarnessUsesEffectiveConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Harness.Default = "codex"
	cfg.Review.Harness = "claude"

	h, err := BuildReviewHarness(&cfg)
	if err != nil {
		t.Fatalf("buildReviewHarness: %v", err)
	}
	if h.Name() != "claude" {
		t.Errorf("review harness = %q, want claude", h.Name())
	}

	cfg.Review.Harness = ""
	h, err = BuildReviewHarness(&cfg)
	if err != nil {
		t.Fatalf("buildReviewHarness fallback: %v", err)
	}
	if h.Name() != "codex" {
		t.Errorf("fallback review harness = %q, want codex", h.Name())
	}
}

func TestVerifyCommands(t *testing.T) {
	cfg := &config.Config{
		Verify: config.Verify{Commands: []string{"go build ./...", "go test ./... -count=1", "golangci-lint run"}},
	}

	got, err := VerifyCommands(cfg, nil, false)
	if err != nil {
		t.Fatalf("verifyCommands: %v", err)
	}
	want := []string{"go build ./...", "go test ./... -count=1", "golangci-lint run"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("verifyCommands = %v, want %v", got, want)
	}

	got, err = VerifyCommands(cfg, []string{"go build ./...", "go test ./..."}, true)
	if err != nil {
		t.Fatalf("verifyCommands flag: %v", err)
	}
	if len(got) != 2 || got[0] != "go build ./..." || got[1] != "go test ./..." {
		t.Errorf("verifyCommands flags = %v, want two commands", got)
	}

	if _, err := VerifyCommands(&config.Config{}, nil, false); err == nil {
		t.Error("VerifyCommands() empty = nil error, want error")
	}
}

func TestBuildHarness(t *testing.T) {
	h, err := buildHarness("claude")
	if err != nil {
		t.Errorf("buildHarness(claude) = %v", err)
	} else if h.Name() != "claude" {
		t.Errorf("buildHarness(claude).Name() = %q, want claude", h.Name())
	}
	h, err = buildHarness("codex")
	if err != nil {
		t.Errorf("buildHarness(codex) = %v", err)
	} else if h.Name() != "codex" {
		t.Errorf("buildHarness(codex).Name() = %q, want codex", h.Name())
	}
	h, err = buildHarness("opencode")
	if err != nil {
		t.Errorf("buildHarness(opencode) = %v", err)
	} else if h.Name() != "opencode" {
		t.Errorf("buildHarness(opencode).Name() = %q, want opencode", h.Name())
	}
	if _, err := buildHarness("bogus"); err == nil {
		t.Error("buildHarness(bogus) = nil error, want unknown-harness")
	}
}

func TestLoadTemplate(t *testing.T) {
	root := t.TempDir()

	got, err := loadTemplate(root, "")
	if err != nil {
		t.Fatalf("loadTemplate default: %v", err)
	}
	if got != prompt.Default() {
		t.Error("loadTemplate default = custom text, want prompt.Default()")
	}

	if err := os.WriteFile(filepath.Join(root, "custom.md"), []byte("CUSTOM {{.Issue}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadTemplate(root, "custom.md")
	if err != nil {
		t.Fatalf("loadTemplate custom: %v", err)
	}
	if got != "CUSTOM {{.Issue}}" {
		t.Errorf("loadTemplate custom = %q", got)
	}

	if _, err := loadTemplate(root, "missing.md"); err == nil {
		t.Error("loadTemplate missing = nil error, want error")
	}
}

func TestResolveBrief(t *testing.T) {
	root := t.TempDir()

	if got, err := resolveBrief(root, ""); err != nil || got != "" {
		t.Errorf("resolveBrief empty = %q, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(root, "DESIGN.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBrief(root, "DESIGN.md")
	if err != nil || got != "DESIGN.md" {
		t.Errorf("resolveBrief = %q, %v", got, err)
	}

	if _, err := resolveBrief(root, "nope.md"); err == nil {
		t.Error("resolveBrief missing = nil error, want error")
	}
}

func TestBuildRunnerWiresConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DESIGN.md"), []byte("brief"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Harness.MaxTurns = 7
	cfg.Review.MaxFixRounds = 3
	cfg.ChangesRequestedLabel = "team-review"
	cfg.Scope.Ignore = []string{"vendor/**"}
	cfg.Prompt.Template = "prompt.md"
	cfg.Prompt.Brief = "DESIGN.md"

	repository := &gitops.Repo{Root: root}
	factory := runnerFactory{
		root:       root,
		config:     &cfg,
		verify:     []string{"go test ./..."},
		harness:    harness.Claude{},
		timeout:    time.Minute,
		repository: repository,
	}
	r, err := factory.build()
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	if r.MaxTurns != 7 {
		t.Errorf("MaxTurns = %d, want 7", r.MaxTurns)
	}
	if r.MaxFixRounds != 3 {
		t.Errorf("MaxFixRounds = %d, want 3", r.MaxFixRounds)
	}
	if r.ChangesRequestedLabel != "team-review" {
		t.Errorf("ChangesRequestedLabel = %q, want team-review", r.ChangesRequestedLabel)
	}
	if len(r.Ignore) != 1 || r.Ignore[0] != "vendor/**" {
		t.Errorf("Ignore = %v, want [vendor/**]", r.Ignore)
	}
	if r.Brief != "DESIGN.md" {
		t.Errorf("Brief = %q, want DESIGN.md", r.Brief)
	}
	if r.Prompt.Template != "custom" {
		t.Errorf("Prompt.Template = %q, want custom", r.Prompt.Template)
	}
}

type refreshOnlyGH struct{ err error }

func (g refreshOnlyGH) Issue(context.Context, string, int) (gh.Issue, error) {
	return gh.Issue{}, g.err
}
func (refreshOnlyGH) Comment(context.Context, string, int, string) error      { return nil }
func (refreshOnlyGH) CommentPR(context.Context, string, string, string) error { return nil }
func (refreshOnlyGH) AddLabel(context.Context, string, int, string) error     { return nil }
func (refreshOnlyGH) RemoveLabel(context.Context, string, int, string) error  { return nil }
func (refreshOnlyGH) CreatePR(context.Context, string, string, string, string, string) (string, error) {
	return "", nil
}

type watchTestGit struct{ *gitops.Repo }

func (watchTestGit) Origin(context.Context) (string, string, error) { return "o", "r", nil }

func concurrentWatchStarts(t *testing.T, factory func() (*runner.Runner, error), jobs int, stopErr error) []error {
	t.Helper()
	errs := make(chan error, jobs)
	for issue := 1; issue <= jobs; issue++ {
		go func(issue int) {
			r, err := factory()
			if err == nil {
				r.GH = refreshOnlyGH{err: stopErr}
				r.Stderr = io.Discard
				_, err = r.Run(context.Background(), issue)
			}
			errs <- err
		}(issue)
	}
	results := make([]error, 0, jobs)
	for range jobs {
		select {
		case err := <-errs:
			results = append(results, err)
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent job startup timed out")
		}
	}
	return results
}

func TestWatchRunnerFactorySerializesConcurrentBaseRefreshes(t *testing.T) {
	fixture := gitfixture.New(t, "trunk")
	cfg := config.Defaults()
	cfg.Base = "trunk"
	const jobs = 16
	stopErr := errors.New("refresh complete")

	fixture.CommitAndPush(t, "trunk", "second")
	shared := watchTestGit{Repo: &gitops.Repo{Root: fixture.Operator}}
	factory := runnerFactory{
		root:       fixture.Operator,
		config:     &cfg,
		verify:     []string{"true"},
		harness:    harness.Claude{},
		timeout:    time.Minute,
		repository: shared,
	}
	first, err := factory.build()
	if err != nil {
		t.Fatalf("first runner: %v", err)
	}
	second, err := factory.build()
	if err != nil {
		t.Fatalf("second runner: %v", err)
	}
	if first.Git != shared || second.Git != shared {
		t.Fatalf("watch runners do not share the injected repository")
	}
	for i, err := range concurrentWatchStarts(t, factory.build, jobs, stopErr) {
		if !errors.Is(err, stopErr) {
			t.Errorf("job %d startup error = %v, want %v", i+1, err, stopErr)
		}
	}
}
