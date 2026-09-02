package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/review"
)

func (r *Runner) review(ctx context.Context, repo string, issueNum int, issue gh.Issue, dir, base string, verification []prompt.VerificationResult, builderReports []prompt.BuilderReport) (review.Outcome, review.Plan, review.PassInstrumentation, error) {
	return r.reviewChanges(ctx, repo, issueNum, issue, dir, base, verification, builderReports, nil, true)
}

func (r *Runner) reviewAfterFix(ctx context.Context, repo string, issueNum int, issue gh.Issue, dir, base string, verification []prompt.VerificationResult, builderReports []prompt.BuilderReport, priorOutcomes []review.Outcome) (review.Outcome, review.Plan, review.PassInstrumentation, error) {
	return r.reviewChanges(ctx, repo, issueNum, issue, dir, base, verification, builderReports, priorOutcomes, false)
}

func (r *Runner) reviewChanges(ctx context.Context, repo string, issueNum int, issue gh.Issue, dir, base string, verification []prompt.VerificationResult, builderReports []prompt.BuilderReport, priorOutcomes []review.Outcome, skipDocsOnly bool) (review.Outcome, review.Plan, review.PassInstrumentation, error) {
	files, err := r.Git.ChangedFiles(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, review.Plan{}, review.PassInstrumentation{}, fmt.Errorf("collect changed files: %w", err)
	}
	plan := review.BuildPlan(files, issueIsBugfix(issue))
	if skipDocsOnly && !plan.HasCode {
		return review.Outcome{}, plan, review.PassInstrumentation{}, nil
	}
	if r.ReviewHarness == nil {
		return review.Outcome{}, plan, review.PassInstrumentation{}, fmt.Errorf("review gate enabled without a review harness")
	}
	diff, err := r.Git.Diff(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, plan, review.PassInstrumentation{}, fmt.Errorf("collect diff: %w", err)
	}
	log, err := r.Git.BranchLog(ctx, dir, base)
	if err != nil {
		return review.Outcome{}, plan, review.PassInstrumentation{}, fmt.Errorf("collect branch log: %w", err)
	}
	conventions := conventionReferences(dir)
	phase := progress.PhaseReviewing
	detail := fmt.Sprintf("reviewer working across %d lenses (read-only)", len(plan.Lenses))
	if !skipDocsOnly {
		phase = progress.PhaseRereviewing
		detail = fmt.Sprintf("reviewer checking fixes across %d lenses (read-only)", len(plan.Lenses))
	}
	r.logf("review: running %s across %d lenses (read-only)", r.ReviewHarness.Name(), len(plan.Lenses))
	r.progress(issueNum, phase, detail, r.ReviewHarness.Name())
	started := time.Now()
	outcomes := make([]review.Outcome, len(plan.Lenses))
	group, groupCtx := errgroup.WithContext(ctx)
	for index, lens := range plan.Lenses {
		index, lens := index, lens
		group.Go(func() (resultErr error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					resultErr = fmt.Errorf("%s review lens panic: %v\n%s", lens.Name, recovered, debug.Stack())
				}
			}()
			contract, err := prompt.RenderReview(prompt.ReviewInput{
				Repository: repo, BaseRef: base, IssueNumber: issueNum, IssueTitle: issue.Title,
				IssueBody: issue.Body, Diff: diff, BranchLog: log, ChangedFiles: files,
				Plan: plan, Lens: lens, VerificationResults: verification, BuilderReports: builderReports,
				PriorOutcomes: priorOutcomes, ConventionReferences: conventions,
			})
			if err != nil {
				return fmt.Errorf("%s review lens: %w", lens.Name, err)
			}
			result, err := r.ReviewHarness.Run(groupCtx, harness.Request{Dir: dir, Prompt: contract, Model: r.ReviewModel, ReadOnly: true})
			if err != nil {
				return fmt.Errorf("%s review lens harness: %w", lens.Name, err)
			}
			outcome, err := review.ParseOutcome(result.Output)
			if err != nil {
				return fmt.Errorf("%s review lens: %w", lens.Name, err)
			}
			outcomes[index] = outcome
			return nil
		})
	}
	err = group.Wait()
	pass := review.PassInstrumentation{DurationMS: time.Since(started).Milliseconds(), LensCount: len(plan.Lenses)}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return review.Outcome{}, plan, pass, fmt.Errorf("%w: review lenses: %v", ErrTimeout, err)
		}
		return review.Outcome{}, plan, pass, err
	}
	outcome := review.MergeOutcomes(outcomes)
	pass = review.InstrumentPass(outcome, time.Duration(pass.DurationMS)*time.Millisecond)
	pass.LensCount = len(plan.Lenses)
	return outcome, plan, pass, nil
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

func reviewPassComment(pass int, outcome review.Outcome) string {
	var body strings.Builder
	fmt.Fprintf(&body, "## Romp review pass %d\n\n**Verdict:** %s", pass, outcome.Verdict)
	if len(outcome.Findings) == 0 {
		body.WriteString("\n\nNo findings.")
	}
	for _, group := range []struct {
		heading  string
		severity review.Severity
	}{
		{heading: "Blocking", severity: review.SeverityBlocking},
		{heading: "Non-blocking", severity: review.SeverityNonBlocking},
		{heading: "Nit", severity: review.SeverityNit},
	} {
		fmt.Fprintf(&body, "\n\n### %s\n\n", group.heading)
		count := 0
		for _, finding := range outcome.Findings {
			if finding.Severity != group.severity {
				continue
			}
			if count > 0 {
				body.WriteByte('\n')
			}
			body.WriteString(formatReviewFinding(finding))
			count++
		}
		if count == 0 {
			body.WriteString("None.")
		}
	}
	return body.String()
}

func formatReviewFinding(finding review.Finding) string {
	location := ""
	if finding.File != nil {
		location = *finding.File
		if finding.Line != nil {
			location += fmt.Sprintf(":%d", *finding.Line)
		}
		location += ": "
	}
	return "- " + location + finding.Description
}

func reviewFailureComment(pass int) string {
	return fmt.Sprintf("## Romp review pass %d\n\n**Review did not complete.**\n\nRomp did not receive a valid review outcome. This failure is not a reviewer finding. See the Romp logs for diagnostic details.", pass)
}
