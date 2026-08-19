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
	"github.com/BRO3886/romp/internal/harness"
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
	return summarizeHarnesses([]harnessProbe{
		probeHarness(ctx, harness.Claude{}),
		probeHarness(ctx, harness.Codex{}),
		probeHarness(ctx, harness.OpenCode{}),
	})
}

// harnessProbe is one adapter's Check outcome.
type harnessProbe struct {
	name   string
	detail string
	err    error
}

func probeHarness(ctx context.Context, h harness.Harness) harnessProbe {
	detail, err := h.Check(ctx)
	return harnessProbe{name: h.Name(), detail: detail, err: err}
}

// summarizeHarnesses passes when at least one probe succeeded. The detail
// lists every healthy harness and, in parentheses, any that failed.
func summarizeHarnesses(probes []harnessProbe) (string, error) {
	var ok, bad []string
	for _, p := range probes {
		if p.err != nil {
			bad = append(bad, p.name+": "+p.err.Error())
			continue
		}
		ok = append(ok, p.name+" "+p.detail)
	}
	if len(ok) == 0 {
		return "", fmt.Errorf("need claude, codex, or opencode: %s", strings.Join(bad, "; "))
	}
	if len(bad) == 0 {
		return strings.Join(ok, "; "), nil
	}
	return strings.Join(ok, "; ") + " (" + strings.Join(bad, "; ") + ")", nil
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
	warnOpenCodeVariant(os.Stderr, cfg)
	verify, err := verifyCommands(cfg, "", false)
	if err != nil {
		return "", err
	}
	return "valid — verify: " + strings.Join(verify, "; "), nil
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
