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
