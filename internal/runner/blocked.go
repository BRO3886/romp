package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	blockedFile  = ".romp/blocked.md"
	blockedLabel = "needs-scoping"
)

// ErrBlocked is returned when the agent stopped because the issue is
// under-scoped. By the time it is returned the issue has been commented on
// and relabelled, and the worktree cleaned up.
var ErrBlocked = errors.New("blocked")

// readBlocked returns the agent-written gap when it stopped because the issue
// is under-scoped, or "" when the agent did not report a block.
func readBlocked(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, blockedFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", blockedFile, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func blockedComment(gap string) string {
	return "romp stopped here because this issue is under-scoped.\n\n" + gap
}
