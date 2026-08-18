// Package prompt renders the goal contract handed to the agent.
package prompt

import (
	"bytes"
	"text/template"
)

// Data holds the values substituted into a prompt template.
type Data struct {
	Issue     string
	Title     string
	Body      string
	Repo      string
	Branch    string
	Base      string
	URL       string
	Verify    string
	Protected string
}

// Renderer renders a goal contract from a text/template string.
type Renderer struct {
	Template string
}

// Render fills the template with d.
func (r Renderer) Render(d Data) (string, error) {
	t, err := template.New("goal").Parse(r.Template)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Default returns the built-in goal-contract template used when no custom
// template is configured.
func Default() string { return defaultTemplate }

const defaultTemplate = `You are an autonomous coding agent fixing an issue in {{.Repo}}.

Work in the git worktree you are already in. The branch is {{.Branch}}, based on {{.Base}}.

Issue #{{.Issue}}: {{.Title}}
{{.URL}}

Issue body:
{{.Body}}

DONE means all of the following hold on a clean working tree:
1. Every acceptance criterion in the issue is met.
2. This command passes: {{.Verify}}

PROVE IT: run ` + "`{{.Verify}}`" + ` yourself before you stop, and show the fresh passing output in your final message. Failing tests are not a stopping condition; fix them and re-run.

CONSTRAINTS:
- Do not delete, skip, or weaken any existing test.
- Do not hardcode expected values just to make a test pass.
- Do not change files outside the scope of this issue.
- Do not run ` + "`git commit`" + `, ` + "`git push`" + `, or ` + "`git worktree`" + ` commands; romp handles those.
{{if .Protected}}Protected paths (do not touch): {{.Protected}}{{end}}

REPORT: before you stop, write your pull request to the file .romp/pull-request.md in the worktree root, in this exact shape:

---
title: <concise PR title>
commit: <conventional commit subject>
---

<PR description>

The commit subject must follow conventional commits: a type prefix (feat, fix, refactor, docs, test, chore) followed by a short description. The description must tell a reviewer what changed and why. If the change is substantial (multiple files, a new abstraction, an architectural decision), include one or more mermaid diagrams (flowchart or sequence) to explain it. For a small change, a few sentences are enough and no diagram is needed.

If the issue is ambiguous, contradictory, or under-specified, stop without writing code. Write the specific gap — what is missing, ambiguous, or contradictory — to the file .romp/blocked.md, then stop.
`
