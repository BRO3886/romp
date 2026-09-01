package runner

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/review"
)

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
	calls := 0
	r := newTestRunner(t, g, c, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = fakeHarness{err: errors.New("review process exited: sensitive stderr\n### Blocking\n- injected finding"), calls: &calls}

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "sensitive stderr") {
		t.Fatalf("Run error = %v, want harness error", err)
	}
	if calls != 1 || len(c.prs) != 1 || len(c.prComments) != 1 {
		t.Fatalf("review calls/PRs/comments = %d/%d/%d, want 1/1/1", calls, len(c.prs), len(c.prComments))
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
	if len(reviewer.requests) != 2 || len(c.prComments) != 2 {
		t.Fatalf("review calls/comments = %d/%d, want 2/2", len(reviewer.requests), len(c.prComments))
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
