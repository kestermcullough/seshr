package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Project struct {
	ID         int64
	Name       string
	Path       string         // as the user added it; used for display
	RealPath   sql.NullString // EvalSymlinks(Path); used for filtering session cwd
	SortOrder  sql.NullInt64
	LastUsedAt sql.NullInt64
	AddedAt    int64
}

// MatchPath is the path the project should be matched against when filtering
// sessions by cwd. Prefers RealPath if it differs from Path (symlinked
// projects), so that sessions whose cwd is the symlink target are found.
func (p Project) MatchPath() string {
	if p.RealPath.Valid && p.RealPath.String != "" {
		return p.RealPath.String
	}
	return p.Path
}

// AddProject saves a new project. The user-typed path is stored in `path`;
// `real_path` is the symlink-resolved version used for matching sessions.
// If the path already exists the existing row is returned untouched.
func (d *DB) AddProject(name, path string) (Project, error) {
	path = canonicalProjectPath(path)
	if name == "" {
		name = filepath.Base(path)
		if name == "/" || name == "." {
			name = path
		}
	}
	realPath := resolveSymlinks(path)
	now := time.Now().Unix()
	_, err := d.sqldb.Exec(
		`INSERT OR IGNORE INTO projects (name, path, real_path, added_at)
              VALUES (?, ?, ?, ?)`,
		name, path, realPath, now,
	)
	if err != nil {
		return Project{}, err
	}
	return d.GetProjectByPath(path)
}

func (d *DB) GetProjectByPath(path string) (Project, error) {
	path = canonicalProjectPath(path)
	row := d.sqldb.QueryRow(
		`SELECT id, name, path, real_path, sort_order, last_used_at, added_at
           FROM projects WHERE path = ?`, path,
	)
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Path, &p.RealPath, &p.SortOrder, &p.LastUsedAt, &p.AddedAt)
	return p, err
}

// ListProjects returns projects in display order: manually-pinned rows
// (sort_order NOT NULL) first by sort_order ascending, then unpinned rows by
// last_used_at descending, with added_at as the tiebreaker.
func (d *DB) ListProjects() ([]Project, error) {
	rows, err := d.sqldb.Query(`
        SELECT id, name, path, real_path, sort_order, last_used_at, added_at
          FROM projects
         ORDER BY
            (sort_order IS NULL) ASC,
            sort_order ASC,
            last_used_at DESC NULLS LAST,
            added_at DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.RealPath, &p.SortOrder, &p.LastUsedAt, &p.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// resolveSymlinks returns the resolved physical path for path, falling back
// to the input if the path doesn't exist or can't be resolved.
func resolveSymlinks(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

// ProjectPathSet returns the union of every project's user-typed path and
// resolved real_path, for "is this dir already a project?" checks.
func (d *DB) ProjectPathSet() (map[string]bool, error) {
	rows, err := d.sqldb.Query(`SELECT path, real_path FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var path string
		var realPath sql.NullString
		if err := rows.Scan(&path, &realPath); err != nil {
			return nil, err
		}
		set[path] = true
		if realPath.Valid && realPath.String != "" {
			set[realPath.String] = true
		}
	}
	return set, rows.Err()
}

// CwdSuggestion is a top-of-modal hint: a cwd that has sessions, ready to
// promote to a project with one Enter.
type CwdSuggestion struct {
	Path  string
	Count int
}

// TopCwdsByRecency returns the most-recently-active session cwds, grouped and
// counted. Used by the add-project modal to surface "where you've already
// been" without making the user navigate there.
func (d *DB) TopCwdsByRecency(limit int) ([]CwdSuggestion, error) {
	rows, err := d.sqldb.Query(`
        SELECT cwd, COUNT(*) AS n
          FROM sessions
         WHERE missing=0 AND archived=0 AND cwd != ''
      GROUP BY cwd
      ORDER BY MAX(last_active) DESC
         LIMIT ?
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CwdSuggestion
	for rows.Next() {
		var s CwdSuggestion
		if err := rows.Scan(&s.Path, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TouchProject bumps last_used_at to now. Called when the user enters a
// project's session view.
func (d *DB) TouchProject(id int64) error {
	_, err := d.sqldb.Exec(`UPDATE projects SET last_used_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	return err
}

func (d *DB) RemoveProject(id int64) error {
	_, err := d.sqldb.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (d *DB) RenameProject(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	_, err := d.sqldb.Exec(`UPDATE projects SET name = ? WHERE id = ?`, name, id)
	return err
}

// MoveProjectUp / MoveProjectDown reorder the projects list by swapping
// sort_order with the adjacent project. The first reorder backfills all
// projects' sort_order with their current display position (so future
// moves are simple swaps).
func (d *DB) MoveProjectUp(id int64) error   { return d.moveProject(id, -1) }
func (d *DB) MoveProjectDown(id int64) error { return d.moveProject(id, 1) }

func (d *DB) moveProject(id int64, delta int) error {
	if err := d.ensureAllPinned(); err != nil {
		return err
	}
	var current sql.NullInt64
	if err := d.sqldb.QueryRow(
		`SELECT sort_order FROM projects WHERE id = ?`, id,
	).Scan(&current); err != nil {
		return err
	}
	if !current.Valid {
		return fmt.Errorf("project not found or not pinned")
	}
	op, orderBy := "<", "DESC"
	if delta > 0 {
		op, orderBy = ">", "ASC"
	}
	var neighborID int64
	var neighborOrder int64
	q := fmt.Sprintf(
		`SELECT id, sort_order FROM projects
           WHERE sort_order %s ?
        ORDER BY sort_order %s LIMIT 1`, op, orderBy,
	)
	err := d.sqldb.QueryRow(q, current.Int64).Scan(&neighborID, &neighborOrder)
	if err == sql.ErrNoRows {
		return nil // at the edge; no-op
	}
	if err != nil {
		return err
	}
	tx, err := d.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE projects SET sort_order = ? WHERE id = ?`, neighborOrder, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET sort_order = ? WHERE id = ?`, current.Int64, neighborID); err != nil {
		return err
	}
	return tx.Commit()
}

// ensureAllPinned backfills sort_order for any project that lacks one,
// numbering rows in current display order with a step of 10 so future
// inserts can squeeze in if we ever need them to.
func (d *DB) ensureAllPinned() error {
	var nullCount int
	if err := d.sqldb.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE sort_order IS NULL`,
	).Scan(&nullCount); err != nil {
		return err
	}
	if nullCount == 0 {
		return nil
	}
	rows, err := d.sqldb.Query(`
        SELECT id FROM projects
         ORDER BY
            (sort_order IS NULL) ASC,
            sort_order ASC,
            last_used_at DESC NULLS LAST,
            added_at DESC
    `)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return err
		}
		ids = append(ids, pid)
	}
	rows.Close()
	tx, err := d.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, pid := range ids {
		if _, err := tx.Exec(
			`UPDATE projects SET sort_order = ? WHERE id = ?`,
			(i+1)*10, pid,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func canonicalProjectPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	return p
}
