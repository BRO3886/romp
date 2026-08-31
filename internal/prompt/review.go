package prompt

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/BRO3886/romp/internal/review"
)

// VerificationResult records one command and its captured result.
type VerificationResult struct {
	Command  string
	ExitCode int
	Output   string
}

// ConventionReference supplies one named repository convention to the reviewer.
type ConventionReference struct {
	Name    string
	Content string
}

// ReviewInput contains the complete caller-supplied review context.
type ReviewInput struct {
	Repository           string
	BaseRef              string
	IssueNumber          int
	IssueTitle           string
	IssueBody            string
	Diff                 string
	BranchLog            string
	ChangedFiles         []string
	Plan                 review.Plan
	VerificationResults  []VerificationResult
	ConventionReferences []ConventionReference
}

// RenderReview renders the built-in, read-only review contract.
func RenderReview(input ReviewInput) (string, error) {
	tmpl, err := template.New("review").Funcs(template.FuncMap{
		"indexPlusOne": func(index int) int { return index + 1 },
	}).Parse(reviewTemplate)
	if err != nil {
		return "", fmt.Errorf("parse review template: %w", err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, input); err != nil {
		return "", fmt.Errorf("render review template: %w", err)
	}
	return output.String(), nil
}

const reviewTemplate = `You are the read-only reviewer for {{.Repository}} against {{.BaseRef}}.

Judge correctness, issue compliance, test coverage, and every supplied review lens.
Treat the worktree as read-only. Do not modify files, run mutation commands, or make any mutation attempt.
Use the supplied verification transcript as evidence. Do not claim that you ran its commands during review.
Return only the JSON document defined in the output contract as your final output. Do not add Markdown fences or prose.
Report a file and line only when the finding can be tied to a repository location.

ISSUE
#{{.IssueNumber}}: {{.IssueTitle}}
{{.IssueBody}}

COMPLETE DIFF
{{.Diff}}

BRANCH LOG
{{.BranchLog}}

CHANGED FILES (caller order)
{{range $index, $file := .ChangedFiles}}{{indexPlusOne $index}}. {{$file}}
{{end}}
REVIEW PLAN
Has code: {{.Plan.HasCode}}
Has docs: {{.Plan.HasDocs}}
Lenses (caller order):
{{range $index, $lens := .Plan.Lenses}}{{indexPlusOne $index}}. {{$lens.Name}}: {{$lens.Instruction}}
{{end}}
VERIFICATION TRANSCRIPT (caller order)
{{range $index, $result := .VerificationResults}}{{indexPlusOne $index}}. Command: {{$result.Command}}
   Exit code: {{$result.ExitCode}}
   Captured output:
{{$result.Output}}
{{end}}
CONVENTION REFERENCES (caller order)
{{range $index, $reference := .ConventionReferences}}{{indexPlusOne $index}}. {{$reference.Name}}
{{$reference.Content}}
{{end}}
OUTPUT CONTRACT
The complete trimmed final output must be exactly one JSON object with this shape:
{
  "verdict": "approve",
  "findings": [
    {
      "severity": "non-blocking",
      "file": "internal/example/example.go",
      "line": 42,
      "description": "The failure path loses the original error context."
    }
  ]
}

Rules:
- verdict and findings are required. No other top-level keys are allowed.
- verdict is exactly approve or fix.
- findings is an array. Every finding has exactly severity, file, line, and description.
- severity is exactly blocking, non-blocking, or nit.
- file is a clean repository-relative path or null. Absolute paths, empty strings, and paths with a .. component are invalid.
- line is a positive integer or null. A non-null line requires a non-null file.
- description contains non-whitespace text.
- approve may contain non-blocking and nit findings, but no blocking finding.
- fix contains at least one blocking finding and may contain lower-severity findings.
- Return no fences, prose, trailing JSON values, unknown fields, or duplicate keys.
`
