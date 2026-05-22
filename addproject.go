package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

type entryKind int

const (
	kindSubdir entryKind = iota
	kindUp
	kindSave
	kindSuggest
)

// addProjectEntry is a single row in the modal's list. Most rows are
// kindSubdir (real directories under the current path); the rest are
// synthetic: kindUp (..), kindSave (commit current dir as project),
// kindSuggest (cwd we already have sessions in, surfaced at the top so
// the user can promote it without navigating).
type addProjectEntry struct {
	kind  entryKind
	name  string // dir name for subdir/up; full path for suggest; sentinel for save
	count int    // sessions under this path (subdir or suggest)
}

type addProjectState struct {
	db         *DB
	dir        string
	filter     textinput.Model
	entries    []os.DirEntry
	cursor     int
	showHidden bool
	err        error

	// Loaded by reset/load against the DB:
	counts       map[string]int  // full-path → recursive session count
	projectPaths map[string]bool // both path and real_path of every project
	suggestions  []CwdSuggestion // top recently-active cwds for quick-add
}

func newAddProject(db *DB) addProjectState {
	in := textinput.New()
	in.Placeholder = "type to filter…"
	in.CharLimit = 256
	return addProjectState{db: db, filter: in}
}

// reset prepares the modal for a fresh open at startDir.
func (a *addProjectState) reset(startDir string) {
	if startDir == "" {
		if c, err := os.Getwd(); err == nil {
			startDir = c
		}
	}
	if startDir == "" {
		startDir = "/"
	}
	a.dir = filepath.Clean(startDir)
	a.filter.SetValue("")
	a.filter.Focus()
	a.cursor = 0
	a.loadProjectMeta()
	a.load()
}

// loadProjectMeta pulls the lookup tables we use for badges and suggestions.
func (a *addProjectState) loadProjectMeta() {
	a.suggestions = nil
	a.projectPaths = map[string]bool{}
	if a.db == nil {
		return
	}
	if sugs, err := a.db.TopCwdsByRecency(8); err == nil {
		a.suggestions = sugs
	}
	if set, err := a.db.ProjectPathSet(); err == nil {
		a.projectPaths = set
	}
}

func (a *addProjectState) load() {
	a.err = nil
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		a.err = err
		a.entries = nil
		a.counts = map[string]int{}
		return
	}
	var dirs []os.DirEntry
	for _, e := range entries {
		if !a.showHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(a.dir, e.Name())); err == nil && info.IsDir() {
				dirs = append(dirs, e)
			}
		}
	}
	a.entries = dirs
	a.loadCounts()
	a.clampCursor()
}

// loadCounts pre-computes, for every visible subdir, how many sessions are
// at or under that directory. One DB query + an in-memory prefix match keeps
// this cheap even on large dirs.
func (a *addProjectState) loadCounts() {
	a.counts = map[string]int{}
	if a.db == nil {
		return
	}
	rows, err := a.db.sqldb.Query(
		`SELECT cwd, COUNT(*) FROM sessions
           WHERE missing=0 AND archived=0 AND cwd != ''
           GROUP BY cwd`,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	type pair struct {
		cwd string
		n   int
	}
	var all []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.cwd, &p.n); err == nil {
			all = append(all, p)
		}
	}
	for _, e := range a.entries {
		ePath := filepath.Join(a.dir, e.Name())
		for _, p := range all {
			if p.cwd == ePath || strings.HasPrefix(p.cwd, ePath+"/") {
				a.counts[ePath] += p.n
			}
		}
	}
}

// visibleEntries returns the rows to render: optional quick-add suggestions
// (only when filter is empty), then "..", then filtered subdirs, then the
// save-current-dir row.
func (a *addProjectState) visibleEntries() []addProjectEntry {
	var out []addProjectEntry

	if a.filter.Value() == "" {
		for _, s := range a.suggestions {
			if a.projectPaths[s.Path] {
				continue
			}
			out = append(out, addProjectEntry{
				kind:  kindSuggest,
				name:  s.Path,
				count: s.Count,
			})
		}
	}

	parent := filepath.Dir(a.dir)
	if parent != a.dir {
		out = append(out, addProjectEntry{kind: kindUp, name: ".."})
	}

	q := strings.ToLower(strings.TrimSpace(a.filter.Value()))
	for _, e := range a.entries {
		if q != "" && !strings.Contains(strings.ToLower(e.Name()), q) {
			continue
		}
		out = append(out, addProjectEntry{kind: kindSubdir, name: e.Name()})
	}

	out = append(out, addProjectEntry{
		kind: kindSave,
		name: "[Save THIS directory: " + a.dir + "]",
	})
	return out
}

func (a *addProjectState) clampCursor() {
	n := len(a.visibleEntries())
	if n == 0 {
		a.cursor = 0
		return
	}
	if a.cursor >= n {
		a.cursor = n - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
}

func (a *addProjectState) moveCursor(delta int) {
	a.cursor += delta
	a.clampCursor()
}

// activateCursor handles Enter on the highlighted row. Returns (saved, savePath)
// where saved=true means the caller should persist savePath as a project.
func (a *addProjectState) activateCursor() (saved bool, savePath string) {
	visible := a.visibleEntries()
	if a.cursor >= len(visible) {
		return false, ""
	}
	e := visible[a.cursor]
	switch e.kind {
	case kindSave:
		return true, a.dir
	case kindSuggest:
		return true, e.name
	case kindUp:
		a.dir = filepath.Dir(a.dir)
		a.filter.SetValue("")
		a.cursor = 0
		a.load()
		return false, ""
	case kindSubdir:
		a.dir = filepath.Join(a.dir, e.name)
		a.filter.SetValue("")
		a.cursor = 0
		a.load()
		return false, ""
	}
	return false, ""
}

func (a *addProjectState) view(termW, termH int) string {
	boxW := clamp(termW*2/3, 60, 110)
	boxH := clamp(termH*3/4, 16, 32)
	innerW := boxW - 6

	header := lipgloss.NewStyle().Bold(true).Render("Add project")
	dirLine := lipgloss.NewStyle().Faint(true).MaxWidth(innerW).Render(a.dir)
	a.filter.Width = innerW - 2
	filterRow := a.filter.View()
	sep := lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", innerW))

	visible := a.visibleEntries()
	maxLines := boxH - 9
	if maxLines < 4 {
		maxLines = 4
	}
	start := 0
	if a.cursor >= maxLines {
		start = a.cursor - maxLines + 1
	}
	end := start + maxLines
	if end > len(visible) {
		end = len(visible)
	}

	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"})
	faintStyle := lipgloss.NewStyle().Faint(true)
	upStyle := lipgloss.NewStyle().Faint(true)
	saveStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
	suggestStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "36"})
	projectTagStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "92", Dark: "141"}).Italic(true)

	var rows []string
	for i := start; i < end; i++ {
		e := visible[i]
		var line string
		switch e.kind {
		case kindUp:
			text := e.name
			if i == a.cursor {
				line = cursorStyle.Render("▶ " + text)
			} else {
				line = "  " + upStyle.Render(text)
			}
		case kindSave:
			if i == a.cursor {
				line = cursorStyle.Render("▶ " + e.name)
			} else {
				line = "  " + saveStyle.Render(e.name)
			}
		case kindSuggest:
			label := "+ " + e.name
			tags := faintStyle.Render(fmt.Sprintf(" · %d session%s", e.count, plural(e.count)))
			if i == a.cursor {
				line = cursorStyle.Render("▶ "+label) + tags
			} else {
				line = "  " + suggestStyle.Render(label) + tags
			}
		default: // kindSubdir
			fullPath := filepath.Join(a.dir, e.name)
			label := e.name + "/"
			var tagsParts []string
			if a.projectPaths[fullPath] {
				tagsParts = append(tagsParts, projectTagStyle.Render("[project]"))
			}
			if c := a.counts[fullPath]; c > 0 {
				tagsParts = append(tagsParts, faintStyle.Render(fmt.Sprintf("%d session%s", c, plural(c))))
			}
			tags := ""
			if len(tagsParts) > 0 {
				tags = "  " + strings.Join(tagsParts, " · ")
			}
			if i == a.cursor {
				line = cursorStyle.Render("▶ "+label) + tags
			} else {
				line = "  " + label + tags
			}
		}
		rows = append(rows, line)
	}
	for len(rows) < maxLines {
		rows = append(rows, "")
	}
	if a.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		rows[0] = errStyle.Render("error: " + a.err.Error())
	}

	help := faintStyle.Render(
		"↑/↓ move · enter open/save · type filter · esc cancel")

	body := strings.Join(append([]string{
		header, dirLine, filterRow, sep,
	}, append(rows, "", help)...), "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Width(boxW).
		BorderForeground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).
		Render(body)

	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, box)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func addedStatus(p Project) string {
	return fmt.Sprintf("added: %s (%s)", p.Name, p.Path)
}
