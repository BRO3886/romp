package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/BRO3886/romp/internal/daemon"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
)

func testDaemon(t *testing.T, store *job.Store) {
	t.Helper()
	if store == nil {
		var err error
		store, err = job.Open(job.Path())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
	}
	dir, err := os.MkdirTemp("/tmp", "romp-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(job.StateDir(), 0700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: protocol.SocketPath(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.New(store, "test").Serve(ctx, ln) }()
	t.Cleanup(func() {
		store.InterruptRunning(context.Background())
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
}

func TestClientHasNoOfflineDatabaseFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := runStatus(context.Background(), os.Stdout, true, true); err == nil {
		t.Fatal("status succeeded with no daemon")
	}
	if _, err := os.Stat(filepath.Join(job.StateDir(), "romp.db")); !os.IsNotExist(err) {
		t.Fatalf("client opened database: %v", err)
	}
}
