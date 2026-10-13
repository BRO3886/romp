package daemon

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/execution"
)

func checkReviewHarness(ctx context.Context, cfg *config.Config) (string, error) {
	if !cfg.Review.Enabled {
		return "disabled", nil
	}
	h, err := execution.BuildReviewHarness(cfg)
	if err != nil {
		return "", err
	}
	detail, err := h.Check(ctx)
	if err != nil {
		return "", fmt.Errorf("review harness %s is unavailable: %w", h.Name(), err)
	}
	return h.Name() + " " + detail, nil
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
