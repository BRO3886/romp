// Package job stores romp's job data in one local SQLite file shared by every
// repo on the machine: the in-flight set (ADR 0005) plus append-only outcome
// history. A row is inserted on claim and moved to the outcome table on
// terminal state, so the jobs table is exactly the set a watcher is currently
// working.
package job

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Job is one in-flight row from the table.
type Job struct {
	Repo      string
	Issue     int
	Branch    string
	ClaimedAt string
	SessionID string
}

// Outcome is one finished job, appended to history on terminal state.
type Outcome struct {
	Repo       string
	Issue      int
	Outcome    string
	Branch     string
	PRURL      string
	Detail     string
	StartedAt  string
	FinishedAt string
	SessionID  string
}

// Store is a thin handle over the SQLite job table.
type Store struct {
	db *sql.DB
}

// Open creates or opens the job table at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	if err := migrateSessionID(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	return &Store{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id         TEXT PRIMARY KEY,
	repo       TEXT NOT NULL,
	issue      INTEGER NOT NULL,
	branch     TEXT NOT NULL,
	claimed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	session_id TEXT,
	UNIQUE(repo, issue)
);
CREATE TABLE IF NOT EXISTS outcomes (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	repo        TEXT NOT NULL,
	issue       INTEGER NOT NULL,
	outcome     TEXT NOT NULL,
	branch      TEXT NOT NULL,
	pr_url      TEXT,
	detail      TEXT,
	started_at  TEXT NOT NULL,
	finished_at TEXT NOT NULL,
	session_id  TEXT
);`

func migrateSessionID(db *sql.DB) error {
	for _, table := range []string{"jobs", "outcomes"} {
		hasColumn, err := tableHasColumn(db, table, "session_id")
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN session_id TEXT`); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("adding %s.session_id: %w", table, err)
		}
	}
	return nil
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close releases the underlying connection.
func (s *Store) Close() error { return s.db.Close() }

// Claim inserts a job row for issue, returning false when another watcher
// already claimed it. The UNIQUE(repo, issue) constraint makes this the
// same-machine serialization point.
func (s *Store) Claim(ctx context.Context, repo string, issue int, branch string) (bool, error) {
	id, err := newID()
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO jobs (id, repo, issue, branch) VALUES (?, ?, ?, ?)`,
		id, repo, issue, branch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// SetSessionID records the harness conversation on an in-flight job.
func (s *Store) SetSessionID(ctx context.Context, repo string, issue int, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is empty")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET session_id = ? WHERE repo = ? AND issue = ?`, sessionID, repo, issue)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("in-flight job %s#%d not found", repo, issue)
	}
	return nil
}

// Delete removes the in-flight row for issue, marking it terminal.
func (s *Store) Delete(ctx context.Context, repo string, issue int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE repo = ? AND issue = ?`, repo, issue)
	return err
}

// ClearRunning removes every in-flight row for repo. A fresh watcher calls
// this on startup for its own repo only, treating a start as proof that
// nothing is in flight there. The repo scope keeps one repo's watcher from
// wiping another repo's in-flight rows in the shared file.
func (s *Store) ClearRunning(ctx context.Context, repo string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE repo = ?`, repo)
	return err
}

// List returns the in-flight rows for repo, or every repo when repo is empty,
// ordered by repo then issue.
func (s *Store) List(ctx context.Context, repo string) ([]Job, error) {
	query := `SELECT repo, issue, branch, claimed_at, COALESCE(session_id, '') FROM jobs`
	var args []any
	if repo != "" {
		query += ` WHERE repo = ?`
		args = append(args, repo)
	}
	query += ` ORDER BY repo, issue`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.Repo, &j.Issue, &j.Branch, &j.ClaimedAt, &j.SessionID); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Finish records the terminal outcome for an in-flight job and removes its
// row in one transaction, carrying the claim timestamp over as the run start.
// A missing in-flight row is a no-op.
func (s *Store) Finish(ctx context.Context, o Outcome) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var startedAt string
	var sessionID sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT claimed_at, session_id FROM jobs WHERE repo = ? AND issue = ?`, o.Repo, o.Issue).Scan(&startedAt, &sessionID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO outcomes (repo, issue, outcome, branch, pr_url, detail, started_at, finished_at, session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.Repo, o.Issue, o.Outcome, o.Branch, nullable(o.PRURL), nullable(o.Detail), startedAt, o.FinishedAt, nullable(sessionID.String)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM jobs WHERE repo = ? AND issue = ?`, o.Repo, o.Issue); err != nil {
		return err
	}
	return tx.Commit()
}

// History returns the most recent limit finished jobs for repo, newest first.
// An empty repo returns outcomes from every repository.
func (s *Store) History(ctx context.Context, repo string, limit int) ([]Outcome, error) {
	query := `SELECT repo, issue, outcome, branch, pr_url, detail, started_at, finished_at, COALESCE(session_id, '') FROM outcomes`
	var args []any
	if repo != "" {
		query += ` WHERE repo = ?`
		args = append(args, repo)
	}
	query += ` ORDER BY finished_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outcome
	for rows.Next() {
		var o Outcome
		var prURL, detail sql.NullString
		if err := rows.Scan(&o.Repo, &o.Issue, &o.Outcome, &o.Branch, &prURL, &detail, &o.StartedAt, &o.FinishedAt, &o.SessionID); err != nil {
			return nil, err
		}
		o.PRURL, o.Detail = prURL.String, detail.String
		out = append(out, o)
	}
	return out, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CountBefore returns how many outcomes finished before before. The cutoff is
// an RFC 3339 timestamp; outcomes store UTC timestamps in a zero-padded form
// that compares lexicographically.
func (s *Store) CountBefore(ctx context.Context, before string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outcomes WHERE finished_at < ?`, before).Scan(&n)
	return n, err
}

// Prune deletes outcomes finished before before and returns how many were
// removed, keeping the history table bounded.
func (s *Store) Prune(ctx context.Context, before string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM outcomes WHERE finished_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Path returns the single job-table file shared by every repo.
func Path() string {
	return filepath.Join(StateDir(), "romp.db")
}

// LogsDir returns the per-job log directory for owner/name under the state dir.
func LogsDir(owner, name string) string {
	return filepath.Join(StateDir(), owner+"-"+name, "logs")
}

// StateDir is the machine-wide romp state directory: XDG_STATE_HOME/romp, or
// ~/.local/state/romp on systems where that is unset (the macOS default).
func StateDir() string {
	return stateDir()
}

func stateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "romp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "romp")
}
