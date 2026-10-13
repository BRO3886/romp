package job

import (
	"context"
	"fmt"
	"github.com/BRO3886/romp/internal/protocol"
	"time"
)

// Repository binds one origin identity to its checkout.
type Repository = protocol.Repository

// Repositories returns the persistent registry in origin order.
func (s *Store) Repositories(ctx context.Context) ([]Repository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT origin, path FROM repositories ORDER BY origin`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Repository{}
	for rows.Next() {
		var r Repository
		if err := rows.Scan(&r.Origin, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Watch registers a checkout, refusing a different checkout of the same origin.
func (s *Store) Watch(ctx context.Context, r Repository) error {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM repositories WHERE origin = ?`, r.Origin).Scan(&path)
	if err == nil {
		if path == r.Path {
			return nil
		}
		return fmt.Errorf("%s is already bound to %s; use rebind", r.Origin, path)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO repositories (origin, path) VALUES (?, ?)`, r.Origin, r.Path)
	return err
}

// Unwatch stops future polling without touching active jobs.
func (s *Store) Unwatch(ctx context.Context, origin string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE origin = ?`, origin)
	return err
}

// Rebind changes the checkout of an existing origin.
func (s *Store) Rebind(ctx context.Context, r Repository) error {
	res, err := s.db.ExecContext(ctx, `UPDATE repositories SET path = ? WHERE origin = ?`, r.Path, r.Origin)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s is not watched", r.Origin)
	}
	return nil
}

// InterruptRunning preserves stale jobs as interrupted outcomes atomically.
func (s *Store) InterruptRunning(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO outcomes
	(repo, issue, outcome, branch, detail, started_at, finished_at, session_id, review_json, builder_harness, reviewer_harness)
	SELECT repo, issue, 'interrupted', branch, 'daemon exited before completion', claimed_at, ?, session_id, review_json, builder_harness, reviewer_harness FROM jobs`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs`); err != nil {
		return err
	}
	return tx.Commit()
}
