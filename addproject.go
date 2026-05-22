package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// addProjectEntry is a single row in the modal's directory listing.
// Most entries are subdirectories; two synthetic entries (".." and the save
// row) bookend the list and are not filtered by the type-to-find input.
type addProjectEntry struct {
	name   string
	isUp   bool
	isSave bool
}

type addProjectState struct {
	dir        string         // currently displayed directory
	filter     textinput.Model // type-to-find input (always focused while modal is open)
	entries    []os.DirEntry  // raw subdirs of `dir` (no synthetic rows)
	cursor     int
	showHidden bool
	err        error
}

func newAddProject() addProjectState {
	in := textinput.New()
	in.Placeholder = "type to filter…"
	in.CharLimit = 256
	return addProjectState{filter: in}
}

// reset prepares the modal for a fresh open at startDir (falls back to cwd → /).
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
	a.load()
}

func (a *addProjectState) load() {
	a.err = nil
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		a.err = err
		a.entries = nil
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
		// Follow symlinks: include if they point at a directory. Broken or
		// non-directory symlinks fall through and are skipped.
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(a.dir, e.Name())); err == nil && info.IsDir() {
				dirs = append(dirs, e)
			}
		}
	}
	a.entries = dirs
	a.clampCursor()
}

// visibleEntries returns "..", then the (filtered) subdirs, then the save row.
// ".." is omitted at the filesystem root.
func (a *addProjectState) visibleEntries() []addProjectEntry {
	var out []addProjectEntry
	parent := filepath.Dir(a.dir)
	if parent != a.dir {
		out = append(out, addProjectEntry{name: "..", isUp: true})
	}
	q := strings.ToLower(strings.TrimSpace(a.filter.Value()))
	for _, e := range a.entries {
		if q != "" && !strings.Contains(strings.ToLower(e.Name()), q) {
			continue
		}
		out = append(out, addProjectEntry{name: e.Name()})
	}
	out = append(out, addProjectEntry{name: "[Save THIS directory: " + a.dir + "]", isSave: true})
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
	switch {
	case e.isSave:
		return true, a.dir
	case e.isUp:
		a.dir = filepath.Dir(a.dir)
		a.filter.SetValue("")
		a.cursor = 0
		a.load()
		return false, ""
	default:
		a.dir = filepath.Join(a.dir, e.name)
		a.filter.SetValue("")
		a.cursor = 0
		a.load()
		return false, ""
	}
}

// view returns the centered modal as a rendered string. Callers should pass
// the full terminal width/height; the modal sizes itself within ~60-75%.
func (a *addProjectState) view(termW, termH int) string {
	boxW := clamp(termW*2/3, 50, 100)
	boxH := clamp(termH*3/4, 14, 30)

	innerW := boxW - 6 // border (2) + padding (4)

	header := lipgloss.NewStyle().Bold(true).Render("Add project")
	dirLine := lipgloss.NewStyle().Faint(true).MaxWidth(innerW).Render(a.dir)
	a.filter.Width = innerW - 2
	filterRow := a.filter.View()
	sep := lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", innerW))

	visible := a.visibleEntries()
	maxLines := boxH - 9 // header, dir, filter, sep, blank, help, blank, borders
	if maxLines < 4 {
		maxLines = 4
	}
	// Scroll window so the cursor stays visible.
	start := 0
	if a.cursor >= maxLines {
		start = a.cursor - maxLines + 1
	}
	end := start + maxLines
	if end > len(visible) {
		end = len(visible)
	}

	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"})
	upStyle := lipgloss.NewStyle().Faint(true)
	saveStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"})

	var rows []string
	for i := start; i < end; i++ {
		e := visible[i]
		var text string
		switch {
		case e.isUp:
			text = "  " + e.name
			if i == a.cursor {
				text = cursorStyle.Render("▶ " + e.name)
			} else {
				text = upStyle.Render(text)
			}
		case e.isSave:
			text = "  " + e.name
			if i == a.cursor {
				text = cursorStyle.Render("▶ " + e.name)
			} else {
				text = saveStyle.Render(text)
			}
		default:
			text = "  " + e.name + "/"
			if i == a.cursor {
				text = cursorStyle.Render("▶ " + e.name + "/")
			}
		}
		rows = append(rows, text)
	}
	for len(rows) < maxLines {
		rows = append(rows, "")
	}
	if a.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		rows[0] = errStyle.Render("error: " + a.err.Error())
	}

	help := lipgloss.NewStyle().Faint(true).Render(
		"↑/↓ move · enter open/save · type filter · esc cancel")

	body := strings.Join(append([]string{
		header,
		dirLine,
		filterRow,
		sep,
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

// helper for status formatting after a successful add
func addedStatus(p Project) string {
	return fmt.Sprintf("added: %s (%s)", p.Name, p.Path)
}
