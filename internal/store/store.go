// Package store persists one sync document per user in a local SQLite database.
// It is deliberately a single-table key/value store keyed by the IAM user id;
// the document body is opaque JSON owned by the client. Optimistic concurrency
// uses a content-derived ETag: a client must have seen the current version
// before it may overwrite it, so a stale device cannot clobber newer data.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver, registered as "sqlite"
)

// ErrConflict is returned by Put when the caller's If-Match ETag does not match
// the stored document's current ETag. The returned Doc carries the current
// server state so the caller can merge and retry.
var ErrConflict = errors.New("etag conflict")

// Doc is a stored sync document.
type Doc struct {
	Body      []byte // opaque JSON
	ETag      string
	UpdatedAt time.Time
}

// Store owns the SQLite connection and the sync_docs table.
type Store struct {
	db *sql.DB
}

// Open opens (creating parent dirs and file if needed) the SQLite database at
// path. WAL + a busy timeout keep the single-writer store responsive under any
// brief request overlap.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is a single-writer engine; capping to one connection avoids
	// "database is locked" under concurrent writers while WAL serves readers.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates the schema if absent. It is idempotent and safe to run on
// every startup.
func (s *Store) Migrate(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS sync_docs (
	uid        TEXT PRIMARY KEY,
	doc        TEXT NOT NULL,
	etag       TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// Get returns the document for uid. ok=false means the user has no document yet.
func (s *Store) Get(ctx context.Context, uid string) (Doc, bool, error) {
	var d Doc
	var updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT doc, etag, updated_at FROM sync_docs WHERE uid = ?`, uid,
	).Scan(&d.Body, &d.ETag, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Doc{}, false, nil
	}
	if err != nil {
		return Doc{}, false, err
	}
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return d, true, nil
}

// Put stores body for uid under optimistic concurrency. ifMatch must equal the
// current ETag ("" when no document exists yet); otherwise ErrConflict is
// returned together with the current document. The read-check-write runs in a
// single transaction so concurrent writers cannot both win.
func (s *Store) Put(ctx context.Context, uid string, body []byte, ifMatch string) (Doc, error) {
	newETag := etagOf(body)
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Doc{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var curETag, curUpdated string
	var curBody []byte
	err = tx.QueryRowContext(ctx,
		`SELECT doc, etag, updated_at FROM sync_docs WHERE uid = ?`, uid,
	).Scan(&curBody, &curETag, &curUpdated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		curETag = "" // no document yet -> caller must present an empty If-Match
	case err != nil:
		return Doc{}, err
	}

	if ifMatch != curETag {
		cur := Doc{Body: curBody, ETag: curETag}
		cur.UpdatedAt, _ = time.Parse(time.RFC3339Nano, curUpdated)
		return cur, ErrConflict
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_docs (uid, doc, etag, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(uid) DO UPDATE SET doc = excluded.doc, etag = excluded.etag, updated_at = excluded.updated_at`,
		uid, string(body), newETag, now.Format(time.RFC3339Nano),
	); err != nil {
		return Doc{}, err
	}
	if err := tx.Commit(); err != nil {
		return Doc{}, err
	}
	return Doc{Body: body, ETag: newETag, UpdatedAt: now}, nil
}

// etagOf derives a stable 128-bit content tag (32 hex chars). Identical content
// yields an identical ETag; any change produces a new one — exactly what the
// If-Match precondition needs.
func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:16])
}
