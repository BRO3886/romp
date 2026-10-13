package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/execution"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/protocol"
	"github.com/BRO3886/romp/internal/watch"
)

type Params = protocol.Params

// RegistryHealth preserves vanished entries with a diagnostic.
type RegistryHealth struct {
	job.Repository
	Health string `json:"health"`
}

func resolveRepository(ctx context.Context, path string) (job.Repository, error) {
	if !filepath.IsAbs(path) {
		return job.Repository{}, fmt.Errorf("checkout path must be absolute")
	}
	root, err := git.FindRoot(ctx, path)
	if err != nil {
		return job.Repository{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return job.Repository{}, err
	}
	owner, name, err := (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return job.Repository{}, err
	}
	return job.Repository{Origin: owner + "/" + name, Path: root}, nil
}

func (s *Server) registry(ctx context.Context) ([]RegistryHealth, error) {
	repos, err := s.Store.Repositories(ctx)
	if err != nil {
		return nil, err
	}
	out := []RegistryHealth{}
	for _, r := range repos {
		health := "ready"
		actual, err := resolveRepository(ctx, r.Path)
		if err != nil {
			health = err.Error()
		} else if actual.Origin != r.Origin {
			health = "checkout origin changed; rebind required"
		}
		out = append(out, RegistryHealth{r, health})
	}
	return out, nil
}

func (s *Server) dispatch(ctx context.Context, peer *peer, method string, raw json.RawMessage) (any, error) {
	var p Params
	if len(raw) != 0 && string(raw) != "null" {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			return nil, &protocol.Error{Code: -32602, Message: "invalid parameters"}
		}
	}
	if method == "daemon.prepareShutdown" {
		if err := s.prepareShutdown(); err != nil {
			return nil, err
		}
		return map[string]bool{"ready": true}, nil
	}
	if method == "daemon.prepareRestart" {
		if err := s.prepareRestart(); err != nil {
			return nil, err
		}
		return protocol.Ready{Ready: true}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch method {
	case "daemon.health", "daemon.version", "daemon.restartReady":
		jobs, err := s.Store.List(ctx, "")
		if err != nil {
			return nil, err
		}
		pressure, pressureErr := s.pressure()
		health := "ready"
		if pressureErr != nil {
			health = pressureErr.Error()
		}
		if s.environment != nil {
			if err := validateEnvironment(*s.environment); err != nil {
				health = err.Error()
			}
		}
		if s.lifecycleErr != nil {
			health = s.lifecycleErr.Error()
		}
		active := max(len(jobs), s.active())
		return protocol.Health{Version: s.Version, ProtocolVersion: protocol.Version, Health: health, MemoryPressure: pressure, ActiveJobs: active, RestartReady: active == 0, Stopping: s.stopping}, nil
	case "subscribe":
		s.peersMu.Lock()
		peer.subscribed = true
		s.peersMu.Unlock()
		return map[string]bool{"subscribed": true}, nil
	case "registry.list":
		return s.registry(ctx)
	case "registry.watch", "registry.rebind":
		if s.stopping {
			return nil, fmt.Errorf("daemon is stopping")
		}
		r, err := resolveRepository(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		if p.Repo != "" && p.Repo != r.Origin {
			return nil, fmt.Errorf("checkout origin %s does not match %s", r.Origin, p.Repo)
		}
		if _, err := execution.Build(ctx, r.Path, config.Overrides{}, nil, false); err != nil {
			return nil, err
		}
		if method == "registry.rebind" {
			if w := s.watchers[r.Origin]; w != nil && w.Active() != 0 {
				return nil, fmt.Errorf("cannot rebind while jobs are active")
			}
			for _, j := range s.manual {
				if j.repo == r.Origin {
					return nil, fmt.Errorf("cannot rebind while foreground run is active")
				}
			}
			err = s.Store.Rebind(ctx, r)
		} else {
			err = s.Store.Watch(ctx, r)
		}
		if err != nil {
			return nil, err
		}
		s.publish("registry.changed", r)
		return r, nil
	case "registry.unwatch":
		if p.Repo == "" {
			return nil, fmt.Errorf("repo is required")
		}
		if err := s.Store.Unwatch(ctx, p.Repo); err != nil {
			return nil, err
		}
		s.publish("registry.changed", map[string]string{"origin": p.Repo, "health": "unwatched"})
		return map[string]bool{"unwatched": true}, nil
	case "jobs.list":
		return s.Store.List(ctx, p.Repo)
	case "jobs.history":
		if p.Limit == 0 {
			p.Limit = 20
		}
		if p.Limit < 1 || p.Limit > 1000 {
			return nil, fmt.Errorf("limit must be between 1 and 1000")
		}
		return s.Store.History(ctx, p.Repo, p.Limit)
	case "jobs.reviewSummary":
		if p.Days < 1 || p.Days > 36500 {
			return nil, fmt.Errorf("days must be between 1 and 36500")
		}
		return s.Store.ReviewSummary(ctx, p.Repo, time.Now().AddDate(0, 0, -p.Days))
	case "jobs.cancel":
		if p.Issue < 1 {
			return nil, fmt.Errorf("positive issue required")
		}
		if w := s.watchers[p.Repo]; w != nil && w.Cancel(p.Issue) {
			return map[string]bool{"cancelled": true}, nil
		}
		if j := s.manual[jobKey(p.Repo, p.Issue)]; j != nil && !j.terminal {
			j.cancel()
			return map[string]bool{"cancelled": true}, nil
		}
		return nil, fmt.Errorf("no active job for %s#%d", p.Repo, p.Issue)
	case "jobs.run":
		return s.startManual(ctx, p)
	case "logs.snapshot", "logs.subscribe":
		return s.logs(ctx, peer, p, method == "logs.subscribe")
	case "doctor":
		return s.doctor(ctx, p.Path)
	case "gc":
		return s.gc(ctx, p)
	default:
		return nil, &protocol.Error{Code: -32601, Message: "method not found"}
	}
}

func (s *Server) tick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	if s.environment != nil {
		if err := validateEnvironment(*s.environment); err != nil {
			s.publish("registry.health", map[string]string{"health": err.Error()})
			return
		}
	}
	pressure, err := s.pressure()
	s.publish("memory.changed", map[string]string{"state": pressure})
	if err != nil {
		log.Printf("memory pressure: %v", err)
		return
	}
	if pressure != "normal" {
		return
	}
	repos, err := s.registry(ctx)
	if err != nil {
		log.Printf("registry: %v", err)
		return
	}
	if len(repos) == 0 {
		return
	}
	start := s.rotation % len(repos)
	s.rotation = (start + 1) % len(repos)
	for i := 0; i < len(repos); i++ {
		r := repos[(start+i)%len(repos)]
		if r.Health != "ready" {
			s.publish("registry.health", r)
			continue
		}
		if s.hasManual(r.Origin) {
			continue
		}
		w := s.watchers[r.Origin]
		if w != nil && s.paths[r.Origin] != r.Path {
			if w.Active() != 0 {
				continue
			}
			w = nil
		}
		if w == nil {
			var err error
			w, err = s.newWatcher(ctx, r.Repository)
			if err != nil {
				r.Health = err.Error()
				s.publish("registry.health", r)
				continue
			}
			s.watchers[r.Origin] = w
			s.paths[r.Origin] = r.Path
		}
		admitted, err := w.PollOne(ctx)
		if err != nil {
			log.Printf("poll %s: %v", r.Origin, err)
		}
		if admitted {
			return
		}
	}
}

func (s *Server) newWatcher(ctx context.Context, repo job.Repository) (*watch.Watcher, error) {
	cfg, err := config.Load(repo.Path, config.Overrides{})
	if err != nil {
		return nil, err
	}
	r, err := execution.Build(ctx, repo.Path, config.Overrides{}, nil, false)
	if err != nil {
		return nil, err
	}
	if _, err := r.Harness.Check(ctx); err != nil {
		return nil, fmt.Errorf("builder preflight: %w", err)
	}
	if _, err := checkReviewHarness(ctx, cfg); err != nil {
		return nil, err
	}
	repository := s.repository(repo.Path)
	w := &watch.Watcher{Repo: repo.Origin, Trigger: cfg.Label, Claim: cfg.ClaimedLabel, Blocked: cfg.BlockedLabel, Width: cfg.Width, GH: &gh.Client{}, Store: s.Store}
	w.AllowClaim = func() bool { pressure, err := s.pressure(); return err == nil && pressure == "normal" }
	w.Progress = func(e progress.Event) { s.publish("jobs.changed", map[string]any{"repo": repo.Origin, "event": e}) }
	w.RunJob = func(ctx context.Context, issue int) (string, error) {
		r, err := execution.Build(ctx, repo.Path, config.Overrides{}, nil, false)
		if err != nil {
			return "", err
		}
		r.Git = repository
		writer, err := s.openLog(repo.Origin, issue)
		if err != nil {
			return "", err
		}
		defer writer.Close()
		r.Codename = codename.For(repo.Origin, issue)
		reviewer := ""
		if r.ReviewEnabled {
			reviewer = r.ReviewHarness.Name()
		}
		if err := s.Store.SetHarnesses(ctx, repo.Origin, issue, r.Harness.Name(), reviewer); err != nil {
			return "", err
		}
		r.Sessions = s.Store
		r.ReviewInstrumentation = s.Store
		r.Progress = w.Progress
		r.Stderr = writer
		return r.Run(ctx, issue)
	}
	w.CleanJob = func(ctx context.Context, issue int) error { return cleanJob(ctx, repo.Path, repo.Origin, issue) }
	w.Logf = func(format string, args ...any) { log.Printf(repo.Origin+": "+format, args...) }
	return w, nil
}

func cleanJob(ctx context.Context, path, repo string, issue int) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	branch := fmt.Sprintf("romp-%d", issue)
	repository := &git.Repo{Root: path}
	if err := repository.RemoveWorktree(ctx, filepath.Join(cache, "romp", strings.ReplaceAll(repo, "/", "-"), branch)); err != nil {
		return err
	}
	return repository.DeleteBranch(ctx, branch)
}

func (s *Server) repository(path string) *git.Repo {
	r := s.repositories[path]
	if r == nil {
		r = &git.Repo{Root: path}
		s.repositories[path] = r
	}
	return r
}

func (s *Server) active() int {
	n := len(s.manual)
	for _, w := range s.watchers {
		n += w.Active()
	}
	return n
}

func (s *Server) prepareShutdown() error { return s.drain(true) }
func (s *Server) prepareRestart() error  { return s.drain(false) }

func (s *Server) drain(cancelJobs bool) error {
	s.mu.Lock()
	s.stopping = true
	errs := []error{s.lifecycleErr}
	watchers := make([]*watch.Watcher, 0, len(s.watchers))
	for _, w := range s.watchers {
		if cancelJobs {
			w.CancelAll()
		}
		watchers = append(watchers, w)
	}
	manual := make([]*manualJob, 0, len(s.manual))
	for _, j := range s.manual {
		if cancelJobs && !j.terminal {
			j.cancel()
		}
		manual = append(manual, j)
	}
	s.mu.Unlock()
	state := "draining for restart"
	if cancelJobs {
		state = "cancelling"
	}
	s.publish("daemon.shutdown", protocol.Shutdown{State: state})
	for _, w := range watchers {
		errs = append(errs, w.Wait())
	}
	for _, j := range manual {
		<-j.done
		errs = append(errs, j.cleanupErr)
	}
	remaining, err := s.Store.List(context.Background(), "")
	errs = append(errs, err)
	if len(remaining) != 0 {
		errs = append(errs, fmt.Errorf("%d job outcomes remain unrecorded", len(remaining)))
	}
	return errors.Join(errs...)
}

type logWriter struct {
	file       *os.File
	server     *Server
	repo, name string
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.server.logMu.Lock()
	defer w.server.logMu.Unlock()
	n, err := w.file.Write(p)
	if n > 0 {
		for data := p[:n]; len(data) > 0; {
			end := min(len(data), 32*1024)
			if end < len(data) {
				for end > 0 && !utf8.RuneStart(data[end]) {
					end--
				}
			}
			if end == 0 {
				end = min(len(data), 32*1024)
			}
			w.server.publishLog(w.repo, w.name, string(data[:end]))
			data = data[end:]
		}
	}
	return n, err
}
func (w *logWriter) Close() error { return w.file.Close() }

func (s *Server) openLog(repo string, issue int) (*logWriter, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("invalid repo")
	}
	dir := job.LogsDir(owner, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	code := codename.For(repo, issue)
	f, err := os.OpenFile(filepath.Join(dir, code+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &logWriter{f, s, repo, code}, nil
}

func (s *Server) publishLog(repo, name, text string) {
	data, _ := json.Marshal(map[string]string{"repo": repo, "name": name, "text": text})
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	for p := range s.peers {
		if p.logRepo != repo || p.logName != name {
			continue
		}
		select {
		case p.queue <- protocol.Message{JSONRPC: "2.0", Method: "logs.record", Params: data}:
		default:
			p.conn.Close()
		}
	}
}

var _ io.WriteCloser = (*logWriter)(nil)
