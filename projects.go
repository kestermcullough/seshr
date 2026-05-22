package main

import (
	"database/sql"
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
