// Control socket: one Unix socket per watched repo under the runtime
// directory, through which clients cancel running jobs. Logs are read straight
// from files, so they need no socket.
package watch

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BRO3886/romp/internal/job"
)

// CancelRequest asks the watcher to kill the job for an issue.
type CancelRequest struct {
	Issue int `json:"issue"`
}

// CancelResponse is the watcher's answer.
type CancelResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// socketPath resolves the control socket for a repo. It lives under
// XDG_RUNTIME_DIR when set, falling back to the state directory so a watcher
// on macOS (where XDG_RUNTIME_DIR is usually unset) still serves one.
func socketPath(repo string) (string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return "", fmt.Errorf("repo %q is not owner/name", repo)
	}
	var dir string
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		dir = filepath.Join(runtime, "romp")
	} else {
		dir = job.StateDir()
	}
	return filepath.Join(dir, owner+"-"+name+".sock"), nil
}

// listen opens the control socket for repo, replacing a stale socket left by
// a crashed watcher. A socket that still answers is a live watcher, so the
// listen fails rather than stealing its jobs.
func listen(repo string) (net.Listener, error) {
	path, err := socketPath(repo)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return nil, fmt.Errorf("socket %s is held by a live watcher", path)
	}
	_ = os.Remove(path)
	return net.Listen("unix", path)
}

// CancelJob asks the watcher for repo to cancel the job for issue.
func CancelJob(repo string, issue int) error {
	path, err := socketPath(repo)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return fmt.Errorf("no romp watcher listening on %s (is `romp watch` running?)", path)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(CancelRequest{Issue: issue}); err != nil {
		return err
	}
	var resp CancelResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
