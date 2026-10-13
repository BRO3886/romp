package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/execution"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
)

type LogSnapshot = protocol.LogSnapshot

func (s *Server) logs(ctx context.Context, peer *peer, p Params, subscribe bool) (any, error) {
	owner, name, ok := strings.Cut(p.Repo, "/")
	if !ok || owner == "" || name == "" || owner == "." || owner == ".." || name == "." || name == ".." || strings.ContainsAny(owner+name, "/\\") {
		return nil, fmt.Errorf("invalid repo")
	}
	if p.Name == "" || filepath.Base(p.Name) != p.Name || strings.ContainsAny(p.Name, "/\\.") {
		return nil, fmt.Errorf("invalid log name")
	}
	if p.Offset < 0 {
		return nil, fmt.Errorf("invalid offset")
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	f, err := os.Open(filepath.Join(job.LogsDir(owner, name), p.Name+".log"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		if !subscribe {
			return nil, fmt.Errorf("no log for %s", p.Name)
		}
		s.peersMu.Lock()
		peer.logRepo = p.Repo
		peer.logName = p.Name
		s.peersMu.Unlock()
		return LogSnapshot{Offset: p.Offset, EOF: true}, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if p.Offset > info.Size() {
		return nil, fmt.Errorf("offset exceeds log size")
	}
	if _, err := f.Seek(p.Offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024))
	if err != nil {
		return nil, err
	}
	next := p.Offset + int64(len(data))
	if subscribe && next == info.Size() {
		s.peersMu.Lock()
		peer.logRepo = p.Repo
		peer.logName = p.Name
		s.peersMu.Unlock()
	}
	return LogSnapshot{Text: string(data), Offset: next, EOF: next == info.Size()}, nil
}

type Check = protocol.Check

func (s *Server) doctor(ctx context.Context, path string) (any, error) {
	checks := []Check{}
	add := func(name, detail string, err error) {
		if err != nil {
			detail = err.Error()
		}
		checks = append(checks, Check{Name: name, Detail: detail, OK: err == nil})
	}
	if s.environment != nil {
		add("environment", "installer-resolved paths", validateEnvironment(*s.environment))
	}
	v, err := git.Version(ctx)
	if err == nil && !gitVersionAtLeast(v, 2, 35) {
		err = fmt.Errorf("git 2.35+ is required: %s", v)
	}
	add("git", v, err)
	account, err := (&gh.Client{}).AuthStatus(ctx)
	add("gh", account, err)
	r, err := resolveRepository(ctx, path)
	if err != nil {
		add("config", "", err)
		return checks, nil
	}
	runner, err := execution.Build(ctx, r.Path, config.Overrides{}, nil, false)
	add("config", "valid verification contract", err)
	if err != nil {
		return checks, nil
	}
	for _, h := range []struct {
		name  string
		check func(context.Context) (string, error)
	}{{"builder", runner.Harness.Check}, {"reviewer", func(ctx context.Context) (string, error) {
		if !runner.ReviewEnabled {
			return "disabled", nil
		}
		return runner.ReviewHarness.Check(ctx)
	}}} {
		detail, err := h.check(ctx)
		add(h.name, detail, err)
	}
	for _, name := range []string{"git", "gh", runner.Harness.Name()} {
		path, err := exec.LookPath(name)
		add(name+" path", path, err)
	}
	return checks, nil
}

type GCResult = protocol.GCResult

func (s *Server) gc(ctx context.Context, p Params) (any, error) {
	repo, err := resolveRepository(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(repo.Path, config.Overrides{})
	if err != nil {
		return nil, err
	}
	days := cfg.HistoryDays
	if p.Days >= 0 {
		days = p.Days
	}
	active, err := s.Store.List(ctx, repo.Origin)
	if err != nil {
		return nil, err
	}
	busy := map[int]bool{}
	for _, j := range active {
		busy[j.Issue] = true
	}
	if w := s.watchers[repo.Origin]; w != nil {
		for _, issue := range w.ActiveIssues() {
			busy[issue] = true
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cache, "romp", strings.ReplaceAll(repo.Origin, "/", "-"))
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	result := GCResult{Worktrees: []string{}, Applied: p.Apply}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "romp-") {
			continue
		}
		issue, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "romp-"))
		if err != nil || issue <= 0 || busy[issue] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if p.Apply {
			if err := (&git.Repo{Root: repo.Path}).RemoveWorktree(ctx, path); err != nil {
				return nil, err
			}
		}
		result.Worktrees = append(result.Worktrees, path)
	}
	if days > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
		result.Outcomes, err = s.Store.CountBefore(ctx, cutoff)
		if err != nil {
			return nil, err
		}
		if p.Apply {
			n, err := s.Store.Prune(ctx, cutoff)
			if err != nil {
				return nil, err
			}
			result.Outcomes = int(n)
		}
	}
	return result, nil
}
