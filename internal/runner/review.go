package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/review"
)

func (r *Runner) review(ctx context.Context, repo string, issueNum int, issue gh.Issue, dir, base string, verification []prompt.VerificationResult) (review.Outcome, review.Plan, error) {
	files, err := r.Git.ChangedFiles(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, review.Plan{}, fmt.Errorf("collect changed files: %w", err)
	}
	plan := review.BuildPlan(files, issueIsBugfix(issue))
	if !plan.HasCode {
		return review.Outcome{Verdict: review.VerdictApprove}, plan, nil
	}
	if r.ReviewHarness == nil {
		return review.Outcome{}, plan, fmt.Errorf("review gate enabled without a review harness")
	}
	diff, err := r.Git.Diff(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, plan, fmt.Errorf("collect diff: %w", err)
	}
	log, err := r.Git.BranchLog(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, plan, fmt.Errorf("collect branch log: %w", err)
	}
	contract, err := prompt.RenderReview(prompt.ReviewInput{
		Repository: repo, BaseRef: base, IssueNumber: issueNum, IssueTitle: issue.Title,
		IssueBody: issue.Body, Diff: diff, BranchLog: log, ChangedFiles: files,
		Plan: plan, VerificationResults: verification, ConventionReferences: conventionReferences(dir),
	})
	if err != nil {
		return review.Outcome{}, plan, err
	}
	result, err := r.ReviewHarness.Run(ctx, harness.Request{Dir: dir, Prompt: contract, Model: r.ReviewModel, ReadOnly: true})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return review.Outcome{}, plan, fmt.Errorf("%w: review harness: %v", ErrTimeout, err)
		}
		return review.Outcome{}, plan, fmt.Errorf("review harness: %w", err)
	}
	outcome, err := review.ParseOutcome(result.Output)
	if err != nil {
		return review.Outcome{}, plan, err
	}
	return outcome, plan, nil
}

func conventionReferences(dir string) []prompt.ConventionReference {
	var references []prompt.ConventionReference
	for _, name := range []string{"AGENTS.md", "CONTEXT.md", "CLAUDE.md"} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		references = append(references, prompt.ConventionReference{Name: name, Content: string(content)})
	}
	return references
}

func issueIsBugfix(issue gh.Issue) bool {
	text := strings.ToLower(issue.Title + "\n" + strings.Join(issue.Labels, "\n"))
	return strings.Contains(text, "bug") || strings.Contains(text, "fix")
}

func formatBlockingFindings(findings []review.Finding) string {
	var lines []string
	for _, finding := range findings {
		if finding.Severity != review.SeverityBlocking {
			continue
		}
		location := ""
		if finding.File != nil {
			location = *finding.File
			if finding.Line != nil {
				location += fmt.Sprintf(":%d", *finding.Line)
			}
			location += ": "
		}
		lines = append(lines, "- "+location+finding.Description)
	}
	return strings.Join(lines, "\n")
}

func appendReviewerNotes(body string, findings []review.Finding) string {
	var notes []string
	for _, finding := range findings {
		if finding.Severity == review.SeverityBlocking {
			continue
		}
		notes = append(notes, "- "+finding.Severity.String()+": "+finding.Description)
	}
	if len(notes) == 0 {
		return body
	}
	return strings.TrimSpace(body) + "\n\n## Reviewer notes\n\n" + strings.Join(notes, "\n")
}

func changesRequestedComment(blocking string) string {
	return "romp's review gate still found blocking findings after one fix round.\n\n" + blocking
}
