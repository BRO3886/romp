// Package gh wraps the GitHub CLI (gh) for issue reads and PR creation.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Client talks to GitHub through the gh CLI.
type Client struct{}

// Issue is the subset of a GitHub issue romp needs.
type Issue struct {
	Number int
	Title  string
	Body   string
	URL    string
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// Issue fetches a single issue by number from owner/name.
func (c *Client) Issue(ctx context.Context, repo string, number int) (Issue, error) {
	out, err := c.run(ctx, "issue", "view", strconv.Itoa(number), "--repo", repo,
		"--json", "number,title,body,url")
	if err != nil {
		return Issue{}, err
	}
	var j struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return Issue{}, err
	}
	return Issue{Number: j.Number, Title: j.Title, Body: j.Body, URL: j.URL}, nil
}

// CreatePR opens a pull request and returns its URL.
func (c *Client) CreatePR(ctx context.Context, repo, title, body, head, base string) (string, error) {
	return c.run(ctx, "pr", "create", "--repo", repo, "--base", base, "--head", head,
		"--title", title, "--body", body)
}

// Comment posts a comment on an issue.
func (c *Client) Comment(ctx context.Context, repo string, number int, body string) error {
	_, err := c.run(ctx, "issue", "comment", strconv.Itoa(number), "--repo", repo, "--body", body)
	return err
}

// AddLabel adds a label to an issue.
func (c *Client) AddLabel(ctx context.Context, repo string, number int, label string) error {
	_, err := c.run(ctx, "issue", "edit", strconv.Itoa(number), "--repo", repo, "--add-label", label)
	return err
}

// CreateLabel ensures a label exists on a repo. An existing label is treated
// as success so init is idempotent.
func (c *Client) CreateLabel(ctx context.Context, repo, label string) error {
	_, err := c.run(ctx, "label", "create", label, "--repo", repo)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}
