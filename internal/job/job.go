// Package job stores the in-flight jobs of a single repo in a local SQLite
// table. A row is inserted on claim and deleted on terminal state, so the
// table is exactly the set of jobs a watcher is currently working.
package job

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

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
	return &Store{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id         TEXT PRIMARY KEY,
	repo       TEXT NOT NULL,
	issue      INTEGER NOT NULL,
	branch     TEXT NOT NULL,
	claimed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE(repo, issue)
);`

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

// Delete removes the in-flight row for issue, marking it terminal.
func (s *Store) Delete(ctx context.Context, repo string, issue int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE repo = ? AND issue = ?`, repo, issue)
	return err
}

// ClearRunning removes every in-flight row. A fresh watcher calls this on
// startup, treating a start as proof that nothing is in flight.
func (s *Store) ClearRunning(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs`)
	return err
}

func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Path returns the job-table file for owner/name under the state dir.
func Path(owner, name string) string {
	return filepath.Join(stateDir(), owner+"-"+name, "jobs.db")
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
