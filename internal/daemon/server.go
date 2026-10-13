// Package daemon owns machine-wide scheduling, persistence, and client transport.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
	"github.com/BRO3886/romp/internal/watch"
	"golang.org/x/sys/unix"
)

// Server owns its store and every job until outcome recording and cleanup finish.
type Server struct {
	Store        *job.Store
	Version      string
	mu           sync.Mutex
	watchers     map[string]*watch.Watcher
	paths        map[string]string
	manual       map[string]*manualJob
	stopping     bool
	rotation     int
	pressure     func() (string, error)
	peersMu      sync.Mutex
	peers        map[*peer]bool
	logMu        sync.Mutex
	environment  *Environment
	repositories map[string]*git.Repo
	lifecycleErr error
}

// New creates an idle server. The caller must reconcile interrupted jobs first.
func New(store *job.Store, version string) *Server {
	return &Server{Store: store, Version: version, watchers: map[string]*watch.Watcher{}, paths: map[string]string{}, manual: map[string]*manualJob{}, pressure: memoryPressure, peers: map[*peer]bool{}, repositories: map[string]*git.Repo{}}
}

// Run holds the machine lock before opening storage or replacing a stale socket.
func Run(ctx context.Context, version string) error {
	dir := job.StateDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("rompd already owns this state directory: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	path := protocol.SocketPath()
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refuse to replace non-socket %s", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return err
	}
	defer ln.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.InterruptRunning(ctx); err != nil {
		return fmt.Errorf("recover interrupted jobs: %w", err)
	}
	s := New(store, version)
	if path := os.Getenv("ROMP_DAEMON_ENVIRONMENT"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var env Environment
		if err := json.Unmarshal(data, &env); err != nil {
			return err
		}
		s.environment = &env
	}
	return s.Serve(ctx, ln)
}

// Serve serves initialized clients and polls globally once per minute.
func (s *Server) Serve(ctx context.Context, ln *net.UnixListener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var clients sync.WaitGroup
	acceptDone := make(chan error, 1)
	acceptStopped := make(chan struct{})
	go func() {
		defer close(acceptStopped)
		for {
			conn, err := ln.AcceptUnix()
			if err != nil {
				acceptDone <- err
				return
			}
			if err := peerAllowed(conn); err != nil {
				conn.Close()
				continue
			}
			clients.Add(1)
			go func() { defer clients.Done(); s.servePeer(ctx, conn) }()
		}
	}()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.tick(ctx)
	var serveErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-acceptDone:
			if !errors.Is(err, net.ErrClosed) {
				serveErr = err
			}
			break loop
		case <-ticker.C:
			s.tick(ctx)
		}
	}
	ln.Close()
	<-acceptStopped
	if err := s.prepareShutdown(); err != nil {
		serveErr = errors.Join(serveErr, err)
	}
	s.publish("daemon.shutdown", map[string]string{"state": "stopped"})
	cancel()
	s.peersMu.Lock()
	for p := range s.peers {
		p.conn.Close()
	}
	s.peersMu.Unlock()
	clients.Wait()
	return serveErr
}

type peer struct {
	conn       *net.UnixConn
	mu         sync.Mutex
	queue      chan protocol.Message
	subscribed bool
	logRepo    string
	logName    string
}

func (p *peer) send(m protocol.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return json.NewEncoder(p.conn).Encode(m)
}

func (s *Server) publish(method string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("notification %s: %v", method, err)
		return
	}
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	for p := range s.peers {
		if !p.subscribed {
			continue
		}
		select {
		case p.queue <- protocol.Message{JSONRPC: "2.0", Method: method, Params: data}:
		default:
			p.conn.Close()
		}
	}
}

func (s *Server) servePeer(ctx context.Context, conn *net.UnixConn) {
	p := &peer{conn: conn, queue: make(chan protocol.Message, 256)}
	s.peersMu.Lock()
	s.peers[p] = true
	s.peersMu.Unlock()
	done := make(chan struct{})
	defer func() { close(done); conn.Close(); s.peersMu.Lock(); delete(s.peers, p); s.peersMu.Unlock() }()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				conn.Close()
				return
			case m := <-p.queue:
				if p.send(m) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	initialized := false
	scan := protocol.Scanner(conn)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for scan.Scan() {
		var req protocol.Message
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			p.send(protocol.Message{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &protocol.Error{Code: -32700, Message: "parse error"}})
			continue
		}
		resp := protocol.Message{JSONRPC: "2.0", ID: req.ID}
		var id any
		idErr := json.Unmarshal(req.ID, &id)
		_, numberID := id.(float64)
		_, stringID := id.(string)
		if req.JSONRPC != "2.0" || req.Method == "" || idErr != nil || (!numberID && !stringID) || len(req.Result) != 0 || req.Error != nil {
			resp.ID = json.RawMessage("null")
			resp.Error = &protocol.Error{Code: -32600, Message: "request must include jsonrpc 2.0, method and id"}
		} else if !initialized && req.Method != "initialize" {
			resp.Error = &protocol.Error{Code: -32002, Message: "initialize required"}
		} else if req.Method == "initialize" {
			var init protocol.Initialize
			if err := json.Unmarshal(req.Params, &init); err != nil || initialized || init.ClientName == "" || init.ClientVersion == "" || init.ProtocolMin > protocol.Version || init.ProtocolMax < protocol.Version || init.ProtocolMin < 1 {
				resp.Error = &protocol.Error{Code: -32001, Message: "incompatible client protocol or invalid initialization; update the client"}
			} else {
				initialized = true
				health := "ready"
				if s.environment != nil {
					if err := validateEnvironment(*s.environment); err != nil {
						health = err.Error()
					}
				}
				resp.Result, _ = json.Marshal(protocol.Initialized{DaemonVersion: s.Version, ProtocolVersion: protocol.Version, Capabilities: []string{"registry", "jobs", "logs", "notifications", "doctor", "gc", "lifecycle"}, Health: health})
				conn.SetReadDeadline(time.Time{})
			}
		} else {
			requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			result, err := s.dispatch(requestCtx, p, req.Method, req.Params)
			cancel()
			if err != nil {
				var rpcErr *protocol.Error
				if errors.As(err, &rpcErr) {
					resp.Error = rpcErr
				} else {
					resp.Error = &protocol.Error{Code: -32000, Message: err.Error()}
				}
			} else {
				resp.Result, _ = json.Marshal(result)
			}
		}
		if p.send(resp) != nil {
			return
		}
	}
}
