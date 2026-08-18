package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const prFile = ".romp/pull-request.md"

// prArtifact is the structured PR content the agent writes before stopping.
type prArtifact struct {
	Title  string
	Commit string
	Body   string
}

// readPR reads the agent-written PR artifact from dir, falling back to
// defaults derived from the issue when it is missing or malformed.
func readPR(dir, issueTitle string, issueNum int) (prArtifact, error) {
	data, err := os.ReadFile(filepath.Join(dir, prFile))
	if err != nil {
		if os.IsNotExist(err) {
			return defaultPR(issueTitle, issueNum), nil
		}
		return prArtifact{}, fmt.Errorf("reading %s: %w", prFile, err)
	}
	return parsePR(string(data), issueTitle, issueNum), nil
}

// removePRArtifact deletes the PR artifact so it is not committed into the
// worktree diff.
func removePRArtifact(dir string) error {
	err := os.Remove(filepath.Join(dir, prFile))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", prFile, err)
	}
	return nil
}

func defaultPR(title string, num int) prArtifact {
	return prArtifact{
		Title:  title,
		Commit: fmt.Sprintf("%s (#%d)", title, num),
		Body:   fmt.Sprintf("Closes #%d", num),
	}
}

// parsePR extracts title, commit subject, and body from a frontmatter-prefixed
// markdown document, falling back to defaults for any missing field.
func parsePR(content, issueTitle string, issueNum int) prArtifact {
	a := defaultPR(issueTitle, issueNum)

	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		a.Body = content
		return a
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		a.Body = content
		return a
	}

	for _, line := range lines[1:end] {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "title":
			a.Title = strings.TrimSpace(val)
		case "commit":
			a.Commit = strings.TrimSpace(val)
		}
	}

	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if a.Title == "" {
		a.Title = issueTitle
	}
	if a.Commit == "" {
		a.Commit = fmt.Sprintf("%s (#%d)", issueTitle, issueNum)
	}
	if body == "" {
		body = fmt.Sprintf("Closes #%d", issueNum)
	}
	a.Body = body
	return a
}

// withCloses appends a "Closes #N" footer unless the body already references
// the issue with a closing keyword.
func withCloses(body string, num int) string {
	lower := strings.ToLower(body)
	for _, kw := range []string{"closes #", "fixes #", "resolves #"} {
		if strings.Contains(lower, kw) {
			return body
		}
	}
	return body + "\n\nCloses #" + strconv.Itoa(num)
}
