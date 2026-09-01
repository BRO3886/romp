// Package gh wraps the GitHub CLI (gh) for issue reads, labels, and PR creation.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// maxRateLimitAttempts and rateLimitBackoff bound how long a job survives a
// GitHub rate limit before the underlying call fails for good.
const maxRateLimitAttempts = 3

var rateLimitBackoff = []time.Duration{5 * time.Second, 15 * time.Second}

// Client talks to GitHub through the gh CLI.
type Client struct{}

// Issue is the subset of a GitHub issue romp needs.
type Issue struct {
	Number int
	Title  string
	Body   string
	URL    string
	Labels []string
}

// HasLabel reports whether the issue carries label.
func (i Issue) HasLabel(label string) bool {
	for _, l := range i.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	return retryAttempts(ctx, maxRateLimitAttempts, rateLimitBackoff, func() (string, error) {
		cmd := exec.CommandContext(ctx, "gh", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out)), nil
	})
}

// retryAttempts re-runs fn when it fails with a GitHub rate-limit error,
// waiting backoff[i] before attempt i+1, up to maxAttempts total calls. Any
// other error returns immediately. The backoff is explicit so tests can pass
// durations far shorter than the production schedule.
func retryAttempts(ctx context.Context, maxAttempts int, backoff []time.Duration, fn func() (string, error)) (string, error) {
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		out, err := fn()
		if err == nil || !isRateLimited(err) {
			return out, err
		}
		lastErr = err
		if i < maxAttempts-1 {
			timer := time.NewTimer(backoff[i])
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			}
		}
	}
	return "", lastErr
}

// isRateLimited reports whether a gh error is a GitHub rate-limit failure.
// A bare 403 is deliberately not matched: it also covers permission errors,
// which retrying would never fix.
func isRateLimited(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests")
}

type issueJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (j issueJSON) issue() Issue {
	labels := make([]string, 0, len(j.Labels))
	for _, l := range j.Labels {
		labels = append(labels, l.Name)
	}
	return Issue{Number: j.Number, Title: j.Title, Body: j.Body, URL: j.URL, Labels: labels}
}

// AuthStatus verifies the gh CLI is installed and authenticated, returning the
// account it is logged in as. It fails fast when gh is missing or unauthenticated.
func (c *Client) AuthStatus(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "auth", "status")
	if err != nil {
		return "", err
	}
	if i := strings.Index(out, "account "); i >= 0 {
		rest := strings.TrimSpace(out[i+len("account "):])
		if j := strings.IndexAny(rest, " \n"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			return rest, nil
		}
	}
	first := out
	if j := strings.IndexByte(first, '\n'); j >= 0 {
		first = first[:j]
	}
	return strings.TrimSpace(first), nil
}

// Issue fetches a single issue by number from owner/name.
func (c *Client) Issue(ctx context.Context, repo string, number int) (Issue, error) {
	out, err := c.run(ctx, "issue", "view", strconv.Itoa(number), "--repo", repo,
		"--json", "number,title,body,url,labels")
	if err != nil {
		return Issue{}, err
	}
	var j issueJSON
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return Issue{}, err
	}
	return j.issue(), nil
}

// ListIssues returns the open issues carrying label.
func (c *Client) ListIssues(ctx context.Context, repo, label string) ([]Issue, error) {
	out, err := c.run(ctx, "issue", "list", "--repo", repo, "--label", label,
		"--state", "open", "--json", "number,title,body,url,labels")
	if err != nil {
		return nil, err
	}
	var raw []issueJSON
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(raw))
	for _, j := range raw {
		issues = append(issues, j.issue())
	}
	return issues, nil
}

// OpenPR reports the number of the open pull request whose head is branch,
// or 0 when no such PR exists.
func (c *Client) OpenPR(ctx context.Context, repo, branch string) (int, error) {
	out, err := c.run(ctx, "pr", "list", "--repo", repo, "--head", branch, "--state", "open", "--json", "number")
	if err != nil {
		return 0, err
	}
	return openPRNumber(out)
}

// openPRNumber extracts the first PR number from gh pr list --json number
// output, or 0 when the list is empty.
func openPRNumber(out string) (int, error) {
	var raw []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return 0, err
	}
	if len(raw) == 0 {
		return 0, nil
	}
	return raw[0].Number, nil
}

// CreatePR opens a pull request and returns its URL.
func (c *Client) CreatePR(ctx context.Context, repo, title, body, head, base string) (string, error) {
	return c.run(ctx, "pr", "create", "--repo", repo, "--base", base, "--head", head,
		"--title", title, "--body", body)
}

// CommentPR posts a new comment on a pull request without editing prior comments.
func (c *Client) CommentPR(ctx context.Context, repo, pullRequest, body string) error {
	_, err := c.run(ctx, "pr", "comment", pullRequest, "--repo", repo, "--body", body)
	return err
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

// Assign adds the authenticated user as an assignee.
func (c *Client) Assign(ctx context.Context, repo string, number int) error {
	_, err := c.run(ctx, "issue", "edit", strconv.Itoa(number), "--repo", repo, "--add-assignee", "@me")
	return err
}

// Unassign removes the authenticated user as an assignee.
func (c *Client) Unassign(ctx context.Context, repo string, number int) error {
	_, err := c.run(ctx, "issue", "edit", strconv.Itoa(number), "--repo", repo, "--remove-assignee", "@me")
	return err
}

// RemoveLabel removes a label from an issue.
func (c *Client) RemoveLabel(ctx context.Context, repo string, number int, label string) error {
	_, err := c.run(ctx, "issue", "edit", strconv.Itoa(number), "--repo", repo, "--remove-label", label)
	return err
}

// CreateLabel ensures a label exists on a repo. An existing label is treated
// as success so init is idempotent; a description is applied on create and
// updated via edit so a re-run does not change the color.
func (c *Client) CreateLabel(ctx context.Context, repo, label, description string) error {
	args := []string{"label", "create", label, "--repo", repo}
	if description != "" {
		args = append(args, "--description", description)
	}
	_, err := c.run(ctx, args...)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "already exists") {
		return err
	}
	if description == "" {
		return nil
	}
	_, err = c.run(ctx, "label", "edit", label, "--repo", repo, "--description", description)
	return err
}
