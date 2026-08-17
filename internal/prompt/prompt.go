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

If the issue is ambiguous, contradictory, or under-specified, stop without writing code and explain exactly what is missing or contradictory.
`
