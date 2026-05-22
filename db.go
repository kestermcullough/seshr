package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	sqldb *sql.DB
}

func dbDir() string {
	return filepath.Join(homeDir(), ".local", "share", "agent-sessions")
}

func OpenDB() (*DB, error) {
	dir := dbDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dsn := filepath.Join(dir, "sessions.db") + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, err
	}
	if _, err := sqldb.Exec(schemaSQL); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &DB{sqldb: sqldb}, nil
}

func (d *DB) Close() error { return d.sqldb.Close() }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT    PRIMARY KEY,
    tool          TEXT    NOT NULL,
    session_uuid  TEXT    NOT NULL,
    file_path     TEXT,
    file_size     INTEGER NOT NULL DEFAULT 0,
    cwd           TEXT,
    started_at    INTEGER,
    last_active   INTEGER,
    title         TEXT,
    title_source  TEXT,
    first_msg     TEXT,
    archived      INTEGER NOT NULL DEFAULT 0,
    missing       INTEGER NOT NULL DEFAULT 0,
    opened_at     INTEGER,
    open_count    INTEGER NOT NULL DEFAULT 0,
    indexed_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_last_active ON sessions(last_active DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_cwd         ON sessions(cwd);
CREATE INDEX IF NOT EXISTS idx_sessions_archived    ON sessions(archived);
CREATE INDEX IF NOT EXISTS idx_sessions_missing     ON sessions(missing);

CREATE TABLE IF NOT EXISTS projects (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    path         TEXT    NOT NULL UNIQUE,
    sort_order   INTEGER,
    last_used_at INTEGER,
    added_at     INTEGER NOT NULL
);
`

// SyncSessions upserts discovered records, preserving user-owned columns
// (archived, opened_at, open_count). Sessions in the DB that no longer appear
// in `discovered` get missing=1; rediscovered sessions are reset to missing=0.
func (d *DB) SyncSessions(discovered []Session) error {
	now := time.Now().Unix()
	tx, err := d.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE sessions SET missing=1`); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
        INSERT INTO sessions (
            id, tool, session_uuid, file_path, file_size,
            cwd, started_at, last_active, title, title_source, first_msg,
            indexed_at, missing
        ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0)
        ON CONFLICT(id) DO UPDATE SET
            tool         = excluded.tool,
            session_uuid = excluded.session_uuid,
            file_path    = excluded.file_path,
            file_size    = excluded.file_size,
            cwd          = excluded.cwd,
            started_at   = excluded.started_at,
            last_active  = excluded.last_active,
            title        = excluded.title,
            title_source = excluded.title_source,
            first_msg    = excluded.first_msg,
            indexed_at   = excluded.indexed_at,
            missing      = 0
    `)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range discovered {
		var startedAt, lastActive sql.NullInt64
		if !s.StartedAt.IsZero() {
			startedAt = sql.NullInt64{Int64: s.StartedAt.Unix(), Valid: true}
		}
		if !s.LastActive.IsZero() {
			lastActive = sql.NullInt64{Int64: s.LastActive.Unix(), Valid: true}
		}
		filePath := nullStr(s.FilePath)
		cwd := nullStr(s.CWD)
		title := nullStr(s.Title)
		titleSrc := nullStr(s.TitleSource)
		firstMsg := nullStr(s.FirstMsg)

		if _, err := stmt.Exec(
			s.ID(), s.Tool, s.SessionUUID, filePath, s.Size,
			cwd, startedAt, lastActive, title, titleSrc, firstMsg,
			now,
		); err != nil {
			return fmt.Errorf("upsert %s: %w", s.ID(), err)
		}
	}
	return tx.Commit()
}

// QueryFilter chooses which sessions to return for display.
type QueryFilter struct {
	CWDPrefix    string // empty = no cwd restriction
	ShowArchived bool   // false = exclude archived
	ShowMissing  bool   // false = exclude missing (file vanished / API thread deleted)
}

// Query returns visible sessions ordered by last_active descending.
func (d *DB) Query(f QueryFilter) ([]Session, error) {
	q := `SELECT id, tool, session_uuid, file_path, file_size, cwd,
                 started_at, last_active, title, title_source, first_msg,
                 archived, missing, opened_at, open_count
            FROM sessions
           WHERE 1=1`
	args := []any{}
	if !f.ShowArchived {
		q += ` AND archived = 0`
	}
	if !f.ShowMissing {
		q += ` AND missing = 0`
	}
	if f.CWDPrefix != "" {
		q += ` AND (cwd = ? OR cwd LIKE ?)`
		args = append(args, f.CWDPrefix, f.CWDPrefix+"/%")
	}
	q += ` ORDER BY last_active DESC NULLS LAST`

	rows, err := d.sqldb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var (
			s              Session
			filePath, cwd  sql.NullString
			title, tSrc    sql.NullString
			firstMsg       sql.NullString
			startedAt, la  sql.NullInt64
			openedAt       sql.NullInt64
			archived, miss int
			openCount      int
			_id            string
		)
		if err := rows.Scan(
			&_id, &s.Tool, &s.SessionUUID, &filePath, &s.Size, &cwd,
			&startedAt, &la, &title, &tSrc, &firstMsg,
			&archived, &miss, &openedAt, &openCount,
		); err != nil {
			return nil, err
		}
		s.FilePath = filePath.String
		s.CWD = cwd.String
		s.Title = title.String
		s.TitleSource = tSrc.String
		s.FirstMsg = firstMsg.String
		if startedAt.Valid {
			s.StartedAt = time.Unix(startedAt.Int64, 0)
		}
		if la.Valid {
			s.LastActive = time.Unix(la.Int64, 0)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) Archive(id string) error {
	_, err := d.sqldb.Exec(`UPDATE sessions SET archived=1 WHERE id=?`, id)
	return err
}

func (d *DB) Unarchive(id string) error {
	_, err := d.sqldb.Exec(`UPDATE sessions SET archived=0 WHERE id=?`, id)
	return err
}

func (d *DB) MarkOpened(id string) error {
	_, err := d.sqldb.Exec(
		`UPDATE sessions SET opened_at=?, open_count=open_count+1 WHERE id=?`,
		time.Now().Unix(), id,
	)
	return err
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
