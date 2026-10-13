package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/execution"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/runner"
)

type manualJob struct {
	repo       string
	cancel     context.CancelFunc
	done       chan struct{}
	cleanupErr error
	terminal   bool
}

func jobKey(repo string, issue int) string { return fmt.Sprintf("%s#%d", repo, issue) }
func (s *Server) hasManual(repo string) bool {
	for _, j := range s.manual {
		if j.repo == repo {
			return true
		}
	}
	return false
}

func (s *Server) startManual(ctx context.Context, p Params) (any, error) {
	if s.stopping {
		return nil, fmt.Errorf("daemon is stopping")
	}
	if s.environment != nil {
		if err := validateEnvironment(*s.environment); err != nil {
			return nil, err
		}
	}
	if p.Issue < 1 {
		return nil, fmt.Errorf("positive issue required")
	}
	repo, err := resolveRepository(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	key := jobKey(repo.Origin, p.Issue)
	if s.manual[key] != nil {
		return nil, fmt.Errorf("job already running")
	}
	active, err := s.Store.List(ctx, repo.Origin)
	if err != nil {
		return nil, err
	}
	for _, j := range active {
		if j.Issue == p.Issue {
			return nil, fmt.Errorf("job already running")
		}
	}
	r, err := execution.Build(ctx, repo.Path, config.Overrides(p.Overrides), p.Verify, p.VerifySet)
	if err != nil {
		return nil, err
	}
	r.Git = s.repository(repo.Path)
	writer, err := s.openLog(repo.Origin, p.Issue)
	if err != nil {
		return nil, err
	}
	claimed, err := s.Store.Claim(ctx, repo.Origin, p.Issue, fmt.Sprintf("romp-%d", p.Issue))
	if err != nil || !claimed {
		writer.Close()
		return nil, fmt.Errorf("foreground job admission failed: %v", err)
	}
	r.Stderr = writer
	r.Codename = writer.name
	r.Sessions = s.Store
	r.ReviewInstrumentation = s.Store
	reviewer := ""
	if r.ReviewEnabled {
		reviewer = r.ReviewHarness.Name()
	}
	if err := s.Store.SetHarnesses(ctx, repo.Origin, p.Issue, r.Harness.Name(), reviewer); err != nil {
		writer.Close()
		s.Store.Delete(ctx, repo.Origin, p.Issue)
		return nil, err
	}
	r.Progress = func(e progress.Event) { s.publish("jobs.changed", map[string]any{"repo": repo.Origin, "event": e}) }
	runCtx, cancel := context.WithCancel(context.Background())
	j := &manualJob{repo: repo.Origin, cancel: cancel, done: make(chan struct{})}
	s.manual[key] = j
	go func() {
		defer cancel()
		defer writer.Close()
		url, runErr := r.Run(runCtx, p.Issue)
		s.mu.Lock()
		j.terminal = true
		cancelled := runCtx.Err() != nil
		s.mu.Unlock()
		outcome := "done"
		switch {
		case cancelled:
			outcome = "cancelled"
			if err := cleanJob(context.Background(), repo.Path, repo.Origin, p.Issue); err != nil {
				log.Printf("foreground cleanup: %v", err)
				j.cleanupErr = err
			}
		case errors.Is(runErr, runner.ErrBlocked):
			outcome = "blocked"
		case errors.Is(runErr, runner.ErrNoChanges):
			outcome = "no-changes"
		case errors.Is(runErr, runner.ErrRed):
			outcome = "red"
		case errors.Is(runErr, runner.ErrTimeout):
			outcome = "timeout"
		case errors.Is(runErr, runner.ErrChangesRequested):
			outcome = "changes-requested"
		case runErr != nil:
			outcome = "error"
		}
		detail := ""
		if runErr != nil {
			detail = runErr.Error()
		}
		if j.cleanupErr != nil {
			detail = errors.Join(runErr, j.cleanupErr).Error()
		}
		o := job.Outcome{Repo: repo.Origin, Issue: p.Issue, Branch: fmt.Sprintf("romp-%d", p.Issue), Outcome: outcome, PRURL: url, Detail: detail, FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := s.Store.Finish(context.Background(), o); err != nil {
			log.Printf("foreground history: %v", err)
			j.cleanupErr = errors.Join(j.cleanupErr, err)
		}
		s.publish("jobs.finished", o)
		writer.Close()
		s.mu.Lock()
		s.lifecycleErr = errors.Join(s.lifecycleErr, j.cleanupErr)
		delete(s.manual, key)
		close(j.done)
		s.mu.Unlock()
	}()
	return map[string]any{"repo": repo.Origin, "issue": p.Issue, "name": writer.name}, nil
}
