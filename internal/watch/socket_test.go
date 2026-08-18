package watch

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/gh"
)

func TestSocketPathUnderRuntimeDir(t *testing.T) {
	dir := shortDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", shortDir(t))

	p, err := socketPath("o/r")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "romp", "o-r.sock")
	if p != want {
		t.Errorf("socketPath = %q, want %q", p, want)
	}
}

func TestSocketPathFallsBackToStateDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	state := shortDir(t)
	t.Setenv("XDG_STATE_HOME", state)

	p, err := socketPath("o/r")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "romp", "o-r.sock")
	if p != want {
		t.Errorf("socketPath = %q, want %q", p, want)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	path, err := socketPath("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	ln, err := listen("o/r")
	if err != nil {
		t.Fatalf("listen with stale socket: %v", err)
	}
	ln.Close()
}

func TestListenRefusesLiveSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	path, err := socketPath("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if _, err := listen("o/r"); err == nil {
		t.Fatal("listen = nil, want error when a live watcher holds the socket")
	}
}

func TestCancelJobClient(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	path, err := socketPath("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req CancelRequest
				_ = json.NewDecoder(c).Decode(&req)
				if req.Issue != 7 {
					_ = json.NewEncoder(c).Encode(CancelResponse{Error: "no running job for issue " + string(rune('0'+req.Issue))})
					return
				}
				_ = json.NewEncoder(c).Encode(CancelResponse{OK: true})
			}(conn)
		}
	}()

	if err := CancelJob("o/r", 7); err != nil {
		t.Errorf("CancelJob(7) = %v, want nil", err)
	}
	if err := CancelJob("o/r", 99); err == nil {
		t.Error("CancelJob(99) = nil, want an error")
	}
}

func TestCancelJobWithoutWatcher(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	if err := CancelJob("o/r", 7); err == nil {
		t.Error("CancelJob = nil, want a connection error")
	}
}

func TestCancelViaSocketRecordsCancelledAndCleansUp(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	cleaned := make(chan struct{})
	var cleanMu sync.Mutex
	cleanCalls := 0
	w.CleanJob = func(_ context.Context, issue int) error {
		cleanMu.Lock()
		cleanCalls++
		cleanMu.Unlock()
		close(cleaned)
		return nil
	}
	w.RunJob = func(ctx context.Context, issue int) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}

	ln, err := listen(w.Repo)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go w.serve(ln)

	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	var wg sync.WaitGroup
	wg.Add(1)
	go w.runJob(context.Background(), gh.Issue{Number: 7}, slots, &wg)
	waitRegistered(t, w, 7)

	if err := CancelJob(w.Repo, 7); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	wg.Wait()
	<-cleaned

	store.mu.Lock()
	if len(store.finished) != 1 || store.finished[0].Outcome != "cancelled" {
		t.Errorf("outcomes = %v, want one cancelled", store.finished)
	}
	store.mu.Unlock()

	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp") {
		t.Errorf("trigger label not removed on cancel: %v", removed)
	}
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released on cancel: %v", removed)
	}
	cleanMu.Lock()
	defer cleanMu.Unlock()
	if cleanCalls != 1 {
		t.Errorf("CleanJob calls = %d, want 1", cleanCalls)
	}
}

func TestCancelUnknownIssueReportsError(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortDir(t))
	ghc := &fakeGH{}
	store := newFakeStore()
	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) { return "", nil }

	ln, err := listen(w.Repo)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go w.serve(ln)

	if err := CancelJob(w.Repo, 7); err == nil {
		t.Error("CancelJob(7) = nil, want no-running-job error")
	}
}

func waitRegistered(t *testing.T, w *Watcher, issue int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		_, ok := w.cancels[issue]
		w.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d never registered", issue)
		}
		time.Sleep(time.Millisecond)
	}
}

// shortDir returns a temp dir under /tmp. macOS limits unix socket paths to
// ~104 chars, and t.TempDir()'s /var/folders path blows that limit.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "romp-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}
