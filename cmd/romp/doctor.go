package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
)

// minGitMajor and minGitMinor are the git version romp requires (2.35, the
// first release with reliable worktree support).
const (
	minGitMajor = 2
	minGitMinor = 35
)

// doctorCheck is one item romp doctor verifies. run returns a short
// human-readable detail on success, or an error with an actionable message.
type doctorCheck struct {
	name string
	run  func(ctx context.Context) (detail string, err error)
}

func newDoctorCmd(checks ...doctorCheck) *cobra.Command {
	if len(checks) == 0 {
		checks = standardDoctorChecks()
	}
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check gh auth, harness, git version, and config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.OutOrStdout(), cmd.Context(), checks)
		},
	}
}

func standardDoctorChecks() []doctorCheck {
	return []doctorCheck{
		{name: "git", run: checkGit},
		{name: "gh", run: checkGH},
		{name: "harness", run: checkHarness},
		{name: "config", run: checkConfig},
	}
}

func runDoctor(out io.Writer, ctx context.Context, checks []doctorCheck) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	var failed int
	for _, c := range checks {
		detail, err := c.run(ctx)
		status := "ok"
		if err != nil {
			failed++
			status = "FAIL"
			detail = err.Error()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, detail, status)
	}
	w.Flush()
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	return nil
}

func checkGit(ctx context.Context) (string, error) {
	v, err := git.Version(ctx)
	if err != nil {
		return "", err
	}
	detail := strings.TrimSpace(strings.TrimPrefix(v, "git version "))
	if !gitVersionAtLeast(v, minGitMajor, minGitMinor) {
		return "", fmt.Errorf("git %s is too old; romp needs %d.%d+", detail, minGitMajor, minGitMinor)
	}
	return detail, nil
}

func checkGH(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found on PATH: install GitHub CLI")
	}
	account, err := (&gh.Client{}).AuthStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("gh is not authenticated: run `gh auth login`")
	}
	return "authenticated as " + account, nil
}

func checkHarness(ctx context.Context) (string, error) {
	h, err := buildHarness(doctorConfig(ctx).Harness.Default)
	if err != nil {
		return "", err
	}
	detail, err := h.Check(ctx)
	if err != nil {
		return "", err
	}
	return h.Name() + " " + detail, nil
}

func checkConfig(ctx context.Context) (string, error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return "", err
	}
	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		return "", err
	}
	verify, err := verifyCommands(cfg, "", false)
	if err != nil {
		return "", err
	}
	return "valid — verify: " + strings.Join(verify, "; "), nil
}

// doctorConfig loads the merged config for the current repo, falling back to
// the built-in defaults when romp is run outside a repository so the harness
// check still has a name to test.
func doctorConfig(ctx context.Context) *config.Config {
	root, err := repoRoot(ctx)
	if err != nil {
		cfg := config.Defaults()
		return &cfg
	}
	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		def := config.Defaults()
		return &def
	}
	return cfg
}

// gitVersionAtLeast reports whether a `git --version` string is at least
// minMajor.minMinor. It understands "git version 2.50.1" and the Apple suffix
// form "git version 2.50.1 (Apple Git-155)"; unparseable strings fail closed.
func gitVersionAtLeast(v string, minMajor, minMinor int) bool {
	v = strings.TrimSpace(strings.TrimPrefix(v, "git version "))
	if i := strings.IndexByte(v, '.'); i >= 0 {
		major, err := strconv.Atoi(v[:i])
		if err != nil {
			return false
		}
		if major != minMajor {
			return major > minMajor
		}
		rest := v[i+1:]
		if j := strings.IndexByte(rest, '.'); j >= 0 {
			rest = rest[:j]
		}
		minor, err := strconv.Atoi(rest)
		if err != nil {
			return false
		}
		return minor >= minMinor
	}
	return false
}
