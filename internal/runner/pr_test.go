package runner

import (
	"strings"
	"testing"
)

func TestParsePRFullFrontmatter(t *testing.T) {
	content := "---\ntitle: Add --verify flag\ncommit: feat: add --verify flag\n---\n\n## What\n\nChanged the thing.\n\n" +
		"```mermaid\nflowchart TD\n  A --> B\n```\n"
	a := parsePR(content, "issue title", 7)

	if a.Title != "Add --verify flag" {
		t.Errorf("title = %q, want %q", a.Title, "Add --verify flag")
	}
	if a.Commit != "feat: add --verify flag" {
		t.Errorf("commit = %q, want %q", a.Commit, "feat: add --verify flag")
	}
	if !strings.Contains(a.Body, "mermaid") || !strings.Contains(a.Body, "A --> B") {
		t.Errorf("body lost mermaid content: %q", a.Body)
	}
}

func TestParsePRNoFrontmatterFallsBack(t *testing.T) {
	a := parsePR("just some body text", "issue title", 7)

	if a.Title != "issue title" {
		t.Errorf("title = %q, want %q", a.Title, "issue title")
	}
	if a.Commit != "issue title (#7)" {
		t.Errorf("commit = %q, want %q", a.Commit, "issue title (#7)")
	}
	if a.Body != "just some body text" {
		t.Errorf("body = %q, want %q", a.Body, "just some body text")
	}
}

func TestWithCloses(t *testing.T) {
	if got := withCloses("a body", 7); !strings.Contains(got, "Closes #7") {
		t.Errorf("withCloses(\"a body\", 7) = %q, want it to contain Closes #7", got)
	}
	if got := withCloses("Fixes #7", 7); got != "Fixes #7" {
		t.Errorf("withCloses appended despite existing closing keyword: %q", got)
	}
}

func TestWithAttribution(t *testing.T) {
	const footer = "Created with [romp](https://romp.sidv.dev) 🦦"
	if got := withAttribution("a body"); got != "a body\n\n"+footer {
		t.Errorf("withAttribution = %q, want attribution footer", got)
	}
	if got := withAttribution("a body\n\n" + footer); got != "a body\n\n"+footer {
		t.Errorf("withAttribution duplicated footer: %q", got)
	}
}

func TestPRBody(t *testing.T) {
	const footer = "Created with [romp](https://romp.sidv.dev) 🦦"
	tests := []struct {
		name string
		body string
		num  int
		want string
	}{
		{
			name: "normal description",
			body: "Describe the change.",
			num:  7,
			want: "Describe the change.\n\nCloses #7\n\n" + footer,
		},
		{
			name: "fallback description",
			body: "Closes #7",
			num:  7,
			want: "Closes #7\n\n" + footer,
		},
		{
			name: "empty description",
			body: "",
			num:  7,
			want: "Closes #7\n\n" + footer,
		},
		{
			name: "already attributed",
			body: "Describe the change.\n\nFixes #7\n\n" + footer,
			num:  7,
			want: "Describe the change.\n\nFixes #7\n\n" + footer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prBody(tt.body, tt.num); got != tt.want {
				t.Errorf("prBody(%q, %d) = %q, want %q", tt.body, tt.num, got, tt.want)
			}
		})
	}
}
