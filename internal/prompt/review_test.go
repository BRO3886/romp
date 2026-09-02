package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/review"
)

func TestRenderReviewGolden(t *testing.T) {
	input := reviewInput()

	got, err := prompt.RenderReview(input)
	if err != nil {
		t.Fatalf("RenderReview: %v", err)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "review_prompt.golden"))
	if err != nil {
		t.Fatalf("read golden prompt: %v", err)
	}
	if got != string(want) {
		t.Errorf("rendered review prompt differs from hand-authored contract\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestReviewComponentsSmoke(t *testing.T) {
	rendered, err := prompt.RenderReview(reviewInput())
	if err != nil {
		t.Fatalf("RenderReview: %v", err)
	}
	if rendered == "" {
		t.Fatal("RenderReview returned an empty prompt")
	}

	output := `{"verdict":"approve","findings":[{"severity":"nit","file":null,"line":null,"description":"A small naming improvement is available."}]}`
	got, err := review.ParseOutcome(output)
	if err != nil {
		t.Fatalf("ParseOutcome: %v", err)
	}
	if got.Verdict != review.VerdictApprove || len(got.Findings) != 1 || got.Findings[0].Severity != review.SeverityNit {
		t.Fatalf("ParseOutcome = %+v, want typed approval with one nit", got)
	}
}

func TestRenderReviewPlanUsesSuppliedVerificationTranscript(t *testing.T) {
	input := reviewInput()
	input.Plan = review.BuildPlan([]string{"internal/prompt/review.go"}, false)
	for _, lens := range input.Plan.Lenses {
		if lens.Name == "tests" {
			input.Lens = lens
			break
		}
	}

	rendered, err := prompt.RenderReview(input)
	if err != nil {
		t.Fatalf("RenderReview: %v", err)
	}

	want := "tests: Assess test coverage using the supplied verification transcript as evidence rather than claiming commands were run during review."
	if !strings.Contains(rendered, want) {
		t.Errorf("rendered review prompt does not contain transcript-only tests lens %q", want)
	}
	if strings.Contains(rendered, "Run the suite and print the exit code") {
		t.Error("rendered review prompt instructs the read-only reviewer to run the test suite")
	}
}

func TestRenderReviewIncludesFocusedLensAndReviewHistory(t *testing.T) {
	input := reviewInput()
	file := "internal/runner/runner.go"
	line := 327
	input.Lens = review.Lens{Name: "correctness", Instruction: "Find every correctness defect."}
	input.BuilderReports = []prompt.BuilderReport{
		{Build: 1, Output: "RED: TestReview failed before the change."},
		{Build: 2, Output: "GREEN: TestReview and go test ./... passed."},
	}
	input.PriorOutcomes = []review.Outcome{{
		Verdict: review.VerdictFix,
		Findings: []review.Finding{{
			Severity: review.SeverityBlocking,
			File:     &file, Line: &line,
			Description: "The second review never runs.",
		}},
	}}

	rendered, err := prompt.RenderReview(input)
	if err != nil {
		t.Fatalf("RenderReview: %v", err)
	}
	for _, want := range []string{
		"FOCUSED REVIEW LENS\ncorrectness: Find every correctness defect.",
		"Do not stop after the first finding.",
		"BUILDER COMPLETION REPORTS",
		"Build 1:\nRED: TestReview failed before the change.",
		"Build 2:\nGREEN: TestReview and go test ./... passed.",
		"PRIOR REVIEW PASSES",
		"Pass 1 verdict: fix",
		"internal/runner/runner.go:327: The second review never runs.",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered review prompt missing %q:\n%s", want, rendered)
		}
	}
}

func reviewInput() prompt.ReviewInput {
	return prompt.ReviewInput{
		Repository:  "BRO3886/romp",
		BaseRef:     "origin/main",
		IssueNumber: 31,
		IssueTitle:  "Render and parse reviews",
		IssueBody:   "Keep the review boundary pure.\nReject invalid output.",
		Diff:        "diff --git a/z.go b/z.go\n+added line",
		BranchLog:   "abc1234 feat: add parser\ndef5678 test: cover renderer",
		ChangedFiles: []string{
			"z-last.go",
			"a-first.go",
		},
		Plan: review.Plan{
			HasCode: true,
			HasDocs: true,
			Lenses: []review.Lens{
				{Name: "security", Instruction: "Check trust boundaries."},
				{Name: "correctness", Instruction: "Find contract violations."},
			},
		},
		Lens: review.Lens{Name: "security", Instruction: "Check trust boundaries."},
		VerificationResults: []prompt.VerificationResult{
			{Command: "go test ./... -count=1", ExitCode: 0, Output: "ok github.com/BRO3886/romp/internal/review"},
			{Command: "go vet ./...", ExitCode: 0, Output: "no findings"},
		},
		ConventionReferences: []prompt.ConventionReference{
			{Name: "AGENTS.md", Content: "Research before editing."},
			{Name: "Go style", Content: "Wrap errors with context."},
		},
	}
}
