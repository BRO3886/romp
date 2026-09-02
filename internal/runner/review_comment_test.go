package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/review"
)

type exhaustiveReviewHarness struct {
	mu       sync.Mutex
	requests []harness.Request
}

func (*exhaustiveReviewHarness) Name() string { return "exhaustive" }

func (*exhaustiveReviewHarness) Check(context.Context) (string, error) { return "exhaustive", nil }

func (h *exhaustiveReviewHarness) Run(_ context.Context, req harness.Request) (harness.Result, error) {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	h.mu.Unlock()

	switch {
	case strings.Contains(req.Prompt, "FOCUSED REVIEW LENS\ncorrectness:"):
		return harness.Result{Output: `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The error path is lost."}]}`}, nil
	case strings.Contains(req.Prompt, "FOCUSED REVIEW LENS\nsecurity:"):
		return harness.Result{Output: `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":11,"description":"The credential is logged."}]}`}, nil
	default:
		return harness.Result{Output: `{"verdict":"approve","findings":[]}`}, nil
	}
}

func (h *exhaustiveReviewHarness) recordedRequests() []harness.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]harness.Request(nil), h.requests...)
}

func TestRunAggregatesEveryReviewLensBeforeFixing(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc feat: change a"}
	c := &fakeGH{}
	builder := &sequenceHarness{results: []harness.Result{{Output: "GREEN: go test ./... passed."}}}
	reviewer := &exhaustiveReviewHarness{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	r.MaxFixRounds = 0

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrChangesRequested) {
		t.Fatalf("Run error = %v, want %v", err, ErrChangesRequested)
	}
	requests := reviewer.recordedRequests()
	wantCalls := len(review.BuildPlan([]string{"internal/a.go"}, false).Lenses)
	if len(requests) != wantCalls {
		t.Fatalf("review calls = %d, want one for each of %d lenses", len(requests), wantCalls)
	}
	for _, req := range requests {
		if !strings.Contains(req.Prompt, "BUILDER COMPLETION REPORTS\nBuild 1:\nGREEN: go test ./... passed.") {
			t.Errorf("review prompt missing builder completion report:\n%s", req.Prompt)
		}
	}
	if len(c.prComments) != 1 || !strings.Contains(c.prComments[0], "The error path is lost.") || !strings.Contains(c.prComments[0], "The credential is logged.") {
		t.Errorf("aggregated PR review comment = %v", c.prComments)
	}
}

type historyReviewHarness struct {
	mu       sync.Mutex
	requests []harness.Request
}

func (*historyReviewHarness) Name() string { return "history" }

func (*historyReviewHarness) Check(context.Context) (string, error) { return "history", nil }

func (h *historyReviewHarness) Run(_ context.Context, req harness.Request) (harness.Result, error) {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	h.mu.Unlock()
	if !strings.Contains(req.Prompt, "PRIOR REVIEW PASSES") && strings.Contains(req.Prompt, "FOCUSED REVIEW LENS\ncorrectness:") {
		return harness.Result{Output: `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The error path is lost."}]}`}, nil
	}
	return harness.Result{Output: `{"verdict":"approve","findings":[]}`}, nil
}

func (h *historyReviewHarness) recordedRequests() []harness.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]harness.Request(nil), h.requests...)
}

func TestRunSuppliesBuilderReportsAndPriorFindingsToRereview(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc feat: change a"}
	c := &fakeGH{}
	builder := &sequenceHarness{results: []harness.Result{
		{Output: "RED: focused regression failed.\nGREEN: focused regression passed."},
		{Output: "GREEN: review fix and full suite passed."},
	}}
	reviewer := &historyReviewHarness{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	r.MaxFixRounds = 1

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requests := reviewer.recordedRequests()
	lensCount := len(review.BuildPlan([]string{"internal/a.go"}, false).Lenses)
	if len(requests) != 2*lensCount {
		t.Fatalf("review calls = %d, want %d across two passes", len(requests), 2*lensCount)
	}
	secondPass := 0
	for _, req := range requests {
		if !strings.Contains(req.Prompt, "PRIOR REVIEW PASSES") {
			continue
		}
		secondPass++
		for _, want := range []string{
			"Build 1:\nRED: focused regression failed.\nGREEN: focused regression passed.",
			"Build 2:\nGREEN: review fix and full suite passed.",
			"Pass 1 verdict: fix",
			"internal/a.go:7: The error path is lost.",
		} {
			if !strings.Contains(req.Prompt, want) {
				t.Errorf("second-pass review prompt missing %q:\n%s", want, req.Prompt)
			}
		}
	}
	if secondPass != lensCount {
		t.Errorf("second-pass prompts = %d, want %d", secondPass, lensCount)
	}
}

type failingLensHarness struct {
	cancelled        atomic.Int32
	panicCorrectness bool
}

func (*failingLensHarness) Name() string { return "failing" }

func (*failingLensHarness) Check(context.Context) (string, error) { return "failing", nil }

func (h *failingLensHarness) Run(ctx context.Context, req harness.Request) (harness.Result, error) {
	if strings.Contains(req.Prompt, "FOCUSED REVIEW LENS\ncorrectness:") {
		if h.panicCorrectness {
			panic("reviewer crashed")
		}
		return harness.Result{}, errors.New("correctness unavailable")
	}
	<-ctx.Done()
	h.cancelled.Add(1)
	return harness.Result{}, ctx.Err()
}

func TestRunConvertsReviewLensPanicToFailure(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc feat: change a"}
	c := &fakeGH{}
	reviewer := &failingLensHarness{panicCorrectness: true}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "correctness review lens panic: reviewer crashed") {
		t.Fatalf("Run error = %v, want recovered correctness lens panic", err)
	}
	lensCount := len(review.BuildPlan([]string{"internal/a.go"}, false).Lenses)
	if got := int(reviewer.cancelled.Load()); got != lensCount-1 {
		t.Errorf("cancelled sibling lenses = %d, want %d", got, lensCount-1)
	}
	if len(c.prComments) != 1 || !strings.Contains(c.prComments[0], "Review did not complete") {
		t.Errorf("review failure comments = %v", c.prComments)
	}
}

func TestLiveCodexReviewLensFanout(t *testing.T) {
	if os.Getenv("ROMP_LIVE_REVIEW_TESTS") != "1" {
		t.Skip("set ROMP_LIVE_REVIEW_TESTS=1 to run the authenticated review fan-out test")
	}
	realHome := os.Getenv("HOME")
	g := &fakeGit{
		changed: true,
		files:   []string{"internal/a.go"},
		diff:    "diff --git a/internal/a.go b/internal/a.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/a.go\n@@ -0,0 +1 @@\n+package a",
		log:     "abc feat: add package a",
		onAdd: func(dir string) error {
			if output, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
				return fmt.Errorf("git init: %w: %s", err, output)
			}
			if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "internal", "a.go"), []byte("package a\n"), 0o644); err != nil {
				return err
			}
			return writePR(dir)
		},
	}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	t.Setenv("HOME", realHome)
	r.Harness = fakeHarness{result: harness.Result{Output: "GREEN: the configured verification command passed."}}
	r.ReviewHarness = harness.Codex{Args: []string{"--ephemeral"}}
	r.ReviewEnabled = true
	r.MaxFixRounds = 0
	instrumentation := &fakeReviewStore{}
	r.ReviewInstrumentation = instrumentation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err := r.Run(ctx, 7)
	if err != nil && !errors.Is(err, ErrChangesRequested) {
		t.Fatalf("Run: %v", err)
	}
	lensCount := len(review.BuildPlan(g.files, false).Lenses)
	if len(instrumentation.metrics.Passes) != 1 || instrumentation.metrics.Passes[0].LensCount != lensCount {
		t.Fatalf("review instrumentation = %+v, want one pass across %d lenses", instrumentation.metrics, lensCount)
	}
	if len(c.prComments) != 1 {
		t.Fatalf("PR comments = %d, want one aggregated review pass", len(c.prComments))
	}
}

func TestRunFailsClosedAndCancelsSiblingReviewLenses(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc feat: change a"}
	c := &fakeGH{}
	reviewer := &failingLensHarness{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "correctness review lens harness") || !strings.Contains(err.Error(), "correctness unavailable") {
		t.Fatalf("Run error = %v, want correctness lens failure", err)
	}
	lensCount := len(review.BuildPlan([]string{"internal/a.go"}, false).Lenses)
	if got := int(reviewer.cancelled.Load()); got != lensCount-1 {
		t.Errorf("cancelled sibling lenses = %d, want %d", got, lensCount-1)
	}
	if len(c.prComments) != 1 || !strings.Contains(c.prComments[0], "Review did not complete") || strings.Contains(c.prComments[0], "### Blocking") {
		t.Errorf("review failure comments = %v", c.prComments)
	}
}

func TestReviewPassComment(t *testing.T) {
	fileOnly := "internal/file.go"
	fileAndLine := "internal/line.go"
	line := 27
	tests := []struct {
		name    string
		pass    int
		outcome review.Outcome
		want    string
	}{
		{
			name: "all severities and locations",
			pass: 2,
			outcome: review.Outcome{
				Verdict: review.VerdictFix,
				Findings: []review.Finding{
					{Severity: review.SeverityNit, Description: "Rename the local."},
					{Severity: review.SeverityBlocking, File: &fileAndLine, Line: &line, Description: "Handle the error."},
					{Severity: review.SeverityNonBlocking, File: &fileOnly, Description: "Share the helper."},
				},
			},
			want: "## Romp review pass 2\n\n**Verdict:** fix\n\n### Blocking\n\n- internal/line.go:27: Handle the error.\n\n### Non-blocking\n\n- internal/file.go: Share the helper.\n\n### Nit\n\n- Rename the local.",
		},
		{
			name:    "approval with no findings",
			pass:    1,
			outcome: review.Outcome{Verdict: review.VerdictApprove},
			want:    "## Romp review pass 1\n\n**Verdict:** approve\n\nNo findings.\n\n### Blocking\n\nNone.\n\n### Non-blocking\n\nNone.\n\n### Nit\n\nNone.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewPassComment(tt.pass, tt.outcome); got != tt.want {
				t.Errorf("reviewPassComment() =\n%s\n\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestRunOrdersPullRequestAndReviewPasses(t *testing.T) {
	const fix = `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The error path is lost."}]}`
	const approve = `{"verdict":"approve","findings":[]}`
	var events []string
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, events: &events}
	c := &fakeGH{events: &events}
	builder := &sequenceHarness{results: []harness.Result{{}, {}}, event: "builder", events: &events}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: fix}, {Output: approve}}, event: "review", events: &events}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	var logs strings.Builder
	r.Stderr = &logs
	var progressEvents []progress.Event
	r.Progress = func(event progress.Event) { progressEvents = append(progressEvents, event) }

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"builder", "push", "create-pr", "review", "pr-comment", "builder", "push", "review", "pr-comment"}
	if !slices.Equal(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
	if want := []string{"romp-7", "romp-7"}; !slices.Equal(g.pushed, want) {
		t.Errorf("pushed branches = %v, want %v", g.pushed, want)
	}
	if len(c.prComments) != 2 {
		t.Fatalf("PR comments = %d, want two", len(c.prComments))
	}
	if strings.Count(logs.String(), "review: running sequence across") != 2 || !strings.Contains(logs.String(), "review fix 1/2: running sequence") {
		t.Errorf("review lifecycle logs missing:\n%s", logs.String())
	}
	var reviewStarts, fixStarts int
	for _, event := range progressEvents {
		switch event.Phase {
		case progress.PhaseReviewing, progress.PhaseRereviewing:
			reviewStarts++
			if event.Harness != "sequence" || !strings.Contains(event.Detail, "read-only") {
				t.Errorf("review event = %+v", event)
			}
		case progress.PhaseFixing:
			fixStarts++
			if event.Harness != "sequence" {
				t.Errorf("fix event = %+v", event)
			}
		}
	}
	if reviewStarts != 2 || fixStarts != 1 {
		t.Errorf("review/fix starts = %d/%d, want 2/1", reviewStarts, fixStarts)
	}
	if !strings.Contains(c.prComments[0], "pass 1") || !strings.Contains(c.prComments[0], "The error path is lost.") {
		t.Errorf("pass 1 comment = %q", c.prComments[0])
	}
	if !strings.Contains(c.prComments[1], "pass 2") || !strings.Contains(c.prComments[1], "No findings.") {
		t.Errorf("pass 2 comment = %q", c.prComments[1])
	}
}

func TestRunDoesNotReviewWhenPullRequestCreationFails(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	c := &fakeGH{createPRErr: errors.New("GitHub unavailable")}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: `{"verdict":"approve","findings":[]}`}}}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = reviewer

	url, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "GitHub unavailable") {
		t.Fatalf("Run error = %v, want PR creation error", err)
	}
	if url != "" {
		t.Errorf("Run URL = %q, want none when PR creation fails", url)
	}
	if len(reviewer.requests) != 0 {
		t.Errorf("reviewer calls = %d, want none", len(reviewer.requests))
	}
}

func TestRunPostsReviewFailureWithoutFindings(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	c := &fakeGH{}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: "not json"}}}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = reviewer

	url, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "parse review outcome") {
		t.Fatalf("Run error = %v, want parsing error", err)
	}
	if url != "https://github.com/o/r/pull/1" {
		t.Errorf("Run URL = %q, want the open PR URL", url)
	}
	if len(c.prs) != 1 || len(c.prComments) != 1 {
		t.Fatalf("PRs/comments = %d/%d, want 1/1", len(c.prs), len(c.prComments))
	}
	comment := c.prComments[0]
	if !strings.Contains(comment, "Review did not complete") || strings.Contains(comment, "### Blocking") {
		t.Errorf("review failure comment = %q", comment)
	}
	if len(g.removed) != 0 {
		t.Errorf("removed worktrees = %v, want preserved", g.removed)
	}
}

func TestRunPostsReviewHarnessFailureWithoutFindings(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = fakeHarness{err: errors.New("review process exited: sensitive stderr\n### Blocking\n- injected finding")}

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "sensitive stderr") {
		t.Fatalf("Run error = %v, want harness error", err)
	}
	if len(c.prs) != 1 || len(c.prComments) != 1 {
		t.Fatalf("PRs/comments = %d/%d, want 1/1", len(c.prs), len(c.prComments))
	}
	comment := c.prComments[0]
	if !strings.Contains(comment, "Review did not complete") || strings.Contains(comment, "sensitive stderr") || strings.Contains(comment, "### Blocking") {
		t.Errorf("review failure comment = %q", comment)
	}
}

func TestRunReviewsSecondPassWhenFixLeavesDocsOnlyChanges(t *testing.T) {
	const fix = `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"Remove the broken code."}]}`
	const approve = `{"verdict":"approve","findings":[]}`
	g := &fakeGit{
		changed:       true,
		onAdd:         writePR,
		fileSequences: [][]string{{"internal/a.go", "docs/readme.md"}, {"docs/readme.md"}},
		diff:          "diff --git a/docs/readme.md b/docs/readme.md",
		log:           "abc fix: remove broken code",
	}
	c := &fakeGH{}
	builder := &sequenceHarness{results: []harness.Result{{}, {}}}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: fix}, {Output: approve}}}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewEnabled = true
	r.ReviewHarness = reviewer

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantReviewCalls := len(review.BuildPlan([]string{"internal/a.go", "docs/readme.md"}, false).Lenses) + len(review.BuildPlan([]string{"docs/readme.md"}, false).Lenses)
	if len(reviewer.requests) != wantReviewCalls || len(c.prComments) != 2 {
		t.Fatalf("review calls/comments = %d/%d, want %d/2", len(reviewer.requests), len(c.prComments), wantReviewCalls)
	}
	if !strings.Contains(c.prComments[1], "pass 2") || !strings.Contains(c.prComments[1], "No findings.") {
		t.Errorf("pass 2 comment = %q", c.prComments[1])
	}
}

func TestRunStopsWhenReviewPassCommentFails(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	c := &fakeGH{commentPRErr: errors.New("comment rejected")}
	builder := &sequenceHarness{results: []harness.Result{{}, {}}}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: `{"verdict":"fix","findings":[{"severity":"blocking","file":null,"line":null,"description":"Fix it."}]}`}}}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewEnabled = true
	r.ReviewHarness = reviewer

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "posting review pass 1") {
		t.Fatalf("Run error = %v, want review comment error", err)
	}
	if len(builder.requests) != 1 {
		t.Errorf("builder calls = %d, want no fix round", len(builder.requests))
	}
	if len(c.prs) != 1 || len(g.removed) != 0 {
		t.Errorf("PRs/removed worktrees = %d/%d, want 1/0", len(c.prs), len(g.removed))
	}
}

func TestRunReturnsErrorWhenReviewFailureCommentCannotBePosted(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	c := &fakeGH{commentPRErr: errors.New("comment rejected")}
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = fakeHarness{err: errors.New("review process exited")}

	url, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "review process exited") || !strings.Contains(err.Error(), "posting review failure for pass 1") {
		t.Fatalf("Run error = %v, want review and comment errors", err)
	}
	if url != "https://github.com/o/r/pull/1" || len(g.removed) != 0 {
		t.Errorf("PR URL/removed worktrees = %q/%d, want preserved PR and worktree", url, len(g.removed))
	}
}
