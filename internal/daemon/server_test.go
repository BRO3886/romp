package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
	"github.com/BRO3886/romp/internal/watch"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rompd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s, err := job.Open(filepath.Join(dir, "romp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	server := New(s, "v0.1.0")
	path := filepath.Join(dir, "romp.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return server, path
}

func rpc(t *testing.T, conn net.Conn, id int, method string, p any) protocol.Message {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(protocol.Message{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprint(id)), Method: method, Params: data}); err != nil {
		t.Fatal(err)
	}
	var resp protocol.Message
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestProtocolInitializationAndCompatibility(t *testing.T) {
	_, path := testServer(t)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if r := rpc(t, conn, 1, "jobs.list", nil); r.Error == nil || r.Error.Code != -32002 {
		t.Fatalf("uninitialized request=%+v", r)
	}
	if r := rpc(t, conn, 2, "initialize", protocol.Initialize{ClientName: "future", ClientVersion: "v9", ProtocolMin: 2, ProtocolMax: 3}); r.Error == nil || r.Error.Code != -32001 {
		t.Fatalf("incompatible client=%+v", r)
	}
	for _, fixture := range []struct{ name, version string }{{"romp", "v0.1.0"}, {"romp-app", "v0.1.0"}} {
		c, err := protocol.Dial(context.Background(), path, fixture.name, fixture.version)
		if err != nil {
			t.Fatal(err)
		}
		if c.Info.ProtocolVersion != 1 || c.Info.DaemonVersion != "v0.1.0" {
			t.Fatalf("initialize=%+v", c.Info)
		}
		var health protocol.Health
		if err := c.Call(context.Background(), "daemon.health", nil, &health); err != nil {
			t.Fatal(err)
		}
		if health.Version != "v0.1.0" || !health.RestartReady {
			t.Fatalf("health=%+v", health)
		}
		var jobs []job.Job
		if err := c.Call(context.Background(), "jobs.list", Params{}, &jobs); err != nil {
			t.Fatal(err)
		}
		if err := c.Call(context.Background(), "unknown", nil, nil); err == nil {
			t.Fatal("unknown method accepted")
		}
		if err := c.Call(context.Background(), "gc", map[string]int{"unknown": 1}, nil); err == nil {
			t.Fatal("unknown parameter accepted")
		}
		c.Close()
	}
}

func checkout(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", dir}, {"-C", dir, "remote", "add", "origin", "https://github.com/" + origin + ".git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "romp.toml"), []byte("[verify]\ncommands = [\"true\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegistryWireAndUnhealthyRebind(t *testing.T) {
	s, path := testServer(t)
	c, err := protocol.Dial(context.Background(), path, "test", "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root := checkout(t, "owner/repo")
	var repo job.Repository
	if err := c.Call(context.Background(), "registry.watch", Params{Path: root}, &repo); err != nil {
		t.Fatal(err)
	}
	other := checkout(t, "owner/repo")
	if err := c.Call(context.Background(), "registry.watch", Params{Path: other}, nil); err == nil {
		t.Fatal("duplicate checkout accepted")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	var health []RegistryHealth
	if err := c.Call(context.Background(), "registry.list", nil, &health); err != nil {
		t.Fatal(err)
	}
	if len(health) != 1 || health[0].Health == "ready" {
		t.Fatalf("missing path health=%+v", health)
	}
	if err := c.Call(context.Background(), "registry.rebind", Params{Path: other}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), "registry.unwatch", Params{Repo: repo.Origin}, nil); err != nil {
		t.Fatal(err)
	}
	repos, err := s.Store.Repositories(context.Background())
	if err != nil || len(repos) != 0 {
		t.Fatal(repos, err)
	}
}

type schedulerGH struct {
	mu   sync.Mutex
	next int
}

func (g *schedulerGH) ListIssues(context.Context, string, string) ([]gh.Issue, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return []gh.Issue{{Number: g.next}}, nil
}
func (*schedulerGH) OpenPR(context.Context, string, string) (int, error)    { return 0, nil }
func (*schedulerGH) AddLabel(context.Context, string, int, string) error    { return nil }
func (*schedulerGH) RemoveLabel(context.Context, string, int, string) error { return nil }
func (*schedulerGH) Assign(context.Context, string, int) error              { return nil }
func (*schedulerGH) Unassign(context.Context, string, int) error            { return nil }
func (*schedulerGH) Comment(context.Context, string, int, string) error     { return nil }

func TestGlobalTickAdmissionPressureFairnessAndDrain(t *testing.T) {
	store, err := job.Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := New(store, "test")
	pressure := "normal"
	s.pressure = func() (string, error) { return pressure, nil }
	for _, origin := range []string{"a/repo", "b/repo"} {
		root := checkout(t, origin)
		if err := store.Watch(context.Background(), job.Repository{Origin: origin, Path: root}); err != nil {
			t.Fatal(err)
		}
		w := &watch.Watcher{Repo: origin, Trigger: "romp", Claim: "claimed", Width: 2, GH: &schedulerGH{}, Store: store, RunJob: func(ctx context.Context, _ int) (string, error) { <-ctx.Done(); return "", ctx.Err() }, CleanJob: func(context.Context, int) error { return nil }}
		s.watchers[origin] = w
		s.paths[origin] = root
	}
	defer s.prepareShutdown()
	for _, level := range []string{"warning", "critical"} {
		pressure = level
		s.tick(context.Background())
		if s.active() != 0 {
			t.Fatal("admitted under pressure", level)
		}
	}
	pressure = "normal"
	s.tick(context.Background())
	if s.active() != 1 || s.watchers["a/repo"].Active() != 1 {
		t.Fatal("first tick must admit only first repo")
	}
	s.tick(context.Background())
	if s.active() != 2 || s.watchers["b/repo"].Active() != 1 {
		t.Fatal("second tick must rotate to second repo")
	}
	pressure = "critical"
	s.tick(context.Background())
	if s.active() != 2 {
		t.Fatal("pressure killed running jobs")
	}
	if err := store.Unwatch(context.Background(), "a/repo"); err != nil {
		t.Fatal(err)
	}
	if s.watchers["a/repo"].Active() != 1 {
		t.Fatal("unwatch cancelled active work")
	}
	if err := s.prepareShutdown(); err != nil {
		t.Fatal(err)
	}
	history, err := store.History(context.Background(), "", 20)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	for _, o := range history {
		if o.Outcome != "cancelled" {
			t.Fatalf("shutdown outcome=%+v", o)
		}
	}
	s.tick(context.Background())
	if s.active() != 0 {
		t.Fatal("admitted after shutdown preparation")
	}
}

func TestLogsSnapshotAndSubscriptionHaveNoGap(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s, path := testServer(t)
	w, err := s.openLog("owner/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	c, err := protocol.Dial(context.Background(), path, "test", "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan string, 1)
	c.OnNotification = func(method string, data json.RawMessage) {
		if method == "logs.record" {
			var r struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(data, &r) != nil {
				t.Error("invalid log notification")
			}
			got <- r.Text
			cancel()
		}
	}
	var snapshot LogSnapshot
	if err := c.Call(ctx, "logs.subscribe", Params{Repo: w.repo, Name: w.name}, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Text != "before\n" || snapshot.Offset != 7 || !snapshot.EOF {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if _, err := w.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	c.Listen(ctx)
	if text := <-got; text != "after\n" {
		t.Fatalf("stream=%q", text)
	}
	if err := c.Call(context.Background(), "logs.snapshot", Params{Repo: "../repo", Name: "../../../private"}, nil); err == nil {
		t.Fatal("log traversal accepted")
	}
}

func TestRestartDrainsWithoutCancelling(t *testing.T) {
	store, err := job.Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := New(store, "test")
	finish := make(chan struct{})
	started := make(chan struct{})
	runErr := make(chan error, 1)
	w := &watch.Watcher{Repo: "o/r", Width: 1, GH: &schedulerGH{}, Store: store, RunJob: func(ctx context.Context, _ int) (string, error) {
		close(started)
		<-finish
		runErr <- ctx.Err()
		return "", nil
	}}
	s.watchers["o/r"] = w
	if admitted, err := w.PollOne(context.Background()); !admitted || err != nil {
		t.Fatal(admitted, err)
	}
	<-started
	done := make(chan error, 1)
	go func() { done <- s.prepareRestart() }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		stopping := s.stopping
		s.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restart did not block admission")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("restart acknowledged before jobs drained: %v", err)
	default:
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-runErr; err != nil {
		t.Fatalf("restart cancelled work: %v", err)
	}
	history, err := store.History(context.Background(), "o/r", 20)
	if err != nil || len(history) != 1 || history[0].Outcome != "done" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}
