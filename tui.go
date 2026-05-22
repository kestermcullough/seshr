package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// ── Modes ───────────────────────────────────────────────────────────────────

type tuiMode int

const (
	modePicker tuiMode = iota
	modeAddProject
	modeSessions
)

// ── Picker items ────────────────────────────────────────────────────────────

type pickerKind int

const (
	pickerCurrent pickerKind = iota
	pickerSaved
	pickerAdd
	pickerAll
)

type pickerItem struct {
	kind    pickerKind
	project Project // valid when kind == pickerSaved
	cwd     string  // valid when kind == pickerCurrent
}

func (i pickerItem) Title() string {
	switch i.kind {
	case pickerCurrent:
		return "● Current dir"
	case pickerSaved:
		return i.project.Name
	case pickerAdd:
		return "+ Add project"
	case pickerAll:
		return "○ All sessions"
	}
	return "?"
}

func (i pickerItem) Description() string {
	switch i.kind {
	case pickerCurrent:
		if i.cwd == "" {
			return "(could not detect cwd)"
		}
		return i.cwd
	case pickerSaved:
		return i.project.Path
	case pickerAdd:
		return "save the current dir or another path as a project"
	case pickerAll:
		return "every session across every cwd"
	}
	return ""
}

func (i pickerItem) FilterValue() string { return i.Title() + " " + i.Description() }

// ── Session items ───────────────────────────────────────────────────────────

// Per-tool color chips. AdaptiveColor lets each terminal pick a value with
// decent contrast; the dark variant is what most users will see.
var toolColors = map[string]lipgloss.AdaptiveColor{
	"claude": {Light: "166", Dark: "208"}, // orange
	"codex":  {Light: "30", Dark: "36"},   // teal
	"amp":    {Light: "92", Dark: "141"},  // purple
	"pi":     {Light: "162", Dark: "213"}, // pink
}

// renderToolLabel returns a bold, color-coded "[TOOL]" label padded to a fixed
// visible width so list rows line up regardless of which tool the row is for.
func renderToolLabel(tool string) string {
	label := "[" + strings.ToUpper(tool) + "]"
	style := lipgloss.NewStyle().Bold(true).Width(8)
	if c, ok := toolColors[tool]; ok {
		style = style.Foreground(c)
	}
	return style.Render(label)
}

type sessionItem struct{ s Session }

func (i sessionItem) Title() string {
	t := i.s.Title
	if t == "" {
		t = "(no title)"
	}
	return renderToolLabel(i.s.Tool) + " " + t
}

// Badge styles for archived/missing markers in the row description.
var (
	archivedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).
			Italic(true).
			Render("[archived]")
	missingBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"}).
			Render("[missing]")
)

func (i sessionItem) Description() string {
	when := "—"
	if !i.s.LastActive.IsZero() {
		when = humanTime(i.s.LastActive)
	}
	cwd := i.s.CWD
	if cwd == "" {
		cwd = "?"
	}
	var prefix string
	switch {
	case i.s.Missing:
		prefix = missingBadge + " "
	case i.s.Archived:
		prefix = archivedBadge + " "
	}
	return prefix + when + "  " + cwd
}

func (i sessionItem) FilterValue() string {
	return i.s.Title + " " + i.s.CWD + " " + i.s.Tool + " " + i.s.FirstMsg
}

// ── Model ───────────────────────────────────────────────────────────────────

type tuiModel struct {
	db   *DB
	mode tuiMode

	// picker
	pickerList list.Model

	// add-project modal
	addProject addProjectState

	// sessions
	list         list.Model
	preview      viewport.Model
	searchInput  textinput.Model
	filter       QueryFilter
	allSessions  []Session // unfiltered set most-recently loaded from DB

	// shared
	width, height int
	ready         bool
	status        string
	resume        *Session // set when user picks a session to resume
}

func newTUI(db *DB) tuiModel {
	pl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	pl.Title = "Agent Sessions"
	pl.SetShowStatusBar(false)
	pl.SetFilteringEnabled(true)
	pl.SetShowHelp(false)

	sessionDelegate := list.NewDefaultDelegate()
	// Unset the delegate's title foreground so the per-item ANSI in
	// sessionItem.Title() (the tool-color chip) renders through. Selected
	// rows keep the delegate's accent so selection is still visible.
	sessionDelegate.Styles.NormalTitle = sessionDelegate.Styles.NormalTitle.UnsetForeground()
	sl := list.New(nil, sessionDelegate, 0, 0)
	sl.Title = "Sessions"
	sl.SetShowStatusBar(false)
	sl.SetFilteringEnabled(false) // we drive filtering ourselves via searchInput
	sl.SetShowHelp(false)

	vp := viewport.New(0, 0)
	vp.SetContent("(select a session to preview)")

	si := textinput.New()
	si.Placeholder = "type to search title, cwd, tool, first message…"
	si.Prompt = "🔍 "
	si.CharLimit = 256

	return tuiModel{
		db:          db,
		mode:        modePicker,
		pickerList:  pl,
		list:        sl,
		preview:     vp,
		searchInput: si,
		addProject:  newAddProject(db),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.loadProjectsCmd(), textinput.Blink, tickEvery(pollInterval))
}

const pollInterval = 5 * time.Second

type tickMsg time.Time

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ── Messages ────────────────────────────────────────────────────────────────

type projectsLoadedMsg struct{ projects []Project }
type sessionsLoadedMsg struct {
	sessions []Session
	filter   QueryFilter
}
type archiveDoneMsg struct{ note string }
type refreshDoneMsg struct {
	sessions []Session
	note     string
}
type softRefreshDoneMsg struct{ sessions []Session }
type ampPreviewFetchedMsg struct {
	sessionID string
	err       error
}

func ampFetchCmd(s Session) tea.Cmd {
	return func() tea.Msg {
		_, err := ampThreadFetch(s)
		return ampPreviewFetchedMsg{sessionID: s.ID(), err: err}
	}
}

// ── Commands ────────────────────────────────────────────────────────────────

func (m tuiModel) loadProjectsCmd() tea.Cmd {
	db := m.db
	return func() tea.Msg {
		ps, _ := db.ListProjects()
		return projectsLoadedMsg{projects: ps}
	}
}

func (m tuiModel) loadSessionsCmd(filter QueryFilter, projectID int64) tea.Cmd {
	db := m.db
	return func() tea.Msg {
		// Always discover + sync before querying so newly-active and
		// just-finalized sessions surface without the user having to refresh.
		discovered, _ := DiscoverAll()
		_ = db.SyncSessions(discovered)
		sessions, _ := db.Query(filter)
		if projectID > 0 {
			_ = db.TouchProject(projectID)
		}
		return sessionsLoadedMsg{sessions: sessions, filter: filter}
	}
}

// ── Update dispatch ─────────────────────────────────────────────────────────

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		return m, nil

	case projectsLoadedMsg:
		m.applyProjects(msg.projects)
		return m, nil

	case sessionsLoadedMsg:
		cmd := m.applySessions(msg.sessions, msg.filter)
		m.mode = modeSessions
		m.status = ""
		m.searchInput.Focus()
		return m, tea.Batch(cmd, textinput.Blink)

	case archiveDoneMsg:
		m.status = msg.note
		return m, m.refreshSessions()

	case refreshDoneMsg:
		cmd := m.applySessions(msg.sessions, m.filter)
		if msg.note != "" {
			m.status = msg.note
		}
		return m, cmd

	case softRefreshDoneMsg:
		// Only mutate the list while the user is still looking at it; otherwise
		// silently drop the result.
		if m.mode == modeSessions {
			return m, m.applySessions(msg.sessions, m.filter)
		}
		return m, nil

	case ampPreviewFetchedMsg:
		if it, ok := m.list.SelectedItem().(sessionItem); ok && it.s.ID() == msg.sessionID {
			if msg.err != nil {
				m.preview.SetContent(previewHeader(it.s) + "(amp fetch failed: " + msg.err.Error() + ")\n")
			} else {
				m.preview.SetContent(renderPreview(it.s))
			}
			m.preview.GotoTop()
		}
		return m, nil

	case tickMsg:
		// Always keep ticking. Only do work when in the sessions view.
		if m.mode == modeSessions {
			return m, tea.Batch(m.softRefreshCmd(), tickEvery(pollInterval))
		}
		return m, tickEvery(pollInterval)
	}

	switch m.mode {
	case modePicker:
		return m.updatePicker(msg)
	case modeAddProject:
		return m.updateAddProject(msg)
	case modeSessions:
		return m.updateSessions(msg)
	}
	return m, nil
}

// ── Picker mode ─────────────────────────────────────────────────────────────

func (m tuiModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.pickerList.FilterState() == list.Filtering {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			break
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "enter":
			if it, ok := m.pickerList.SelectedItem().(pickerItem); ok {
				if it.kind != pickerAdd {
					m.status = "scanning…"
				}
				return m.handlePickerEnter(it)
			}
		case "+":
			return m.enterAddMode("")
		}
	}
	var cmd tea.Cmd
	m.pickerList, cmd = m.pickerList.Update(msg)
	return m, cmd
}

func (m tuiModel) handlePickerEnter(it pickerItem) (tea.Model, tea.Cmd) {
	switch it.kind {
	case pickerCurrent:
		if it.cwd == "" {
			m.status = "no current dir; pick All or add a project"
			return m, nil
		}
		return m, m.loadSessionsCmd(QueryFilter{CWDPrefix: it.cwd}, 0)
	case pickerSaved:
		return m, m.loadSessionsCmd(QueryFilter{CWDPrefix: it.project.MatchPath()}, it.project.ID)
	case pickerAdd:
		cwd, _ := os.Getwd()
		return m.enterAddMode(cwd)
	case pickerAll:
		return m, m.loadSessionsCmd(QueryFilter{}, 0)
	}
	return m, nil
}

func (m tuiModel) enterAddMode(startDir string) (tea.Model, tea.Cmd) {
	m.mode = modeAddProject
	m.addProject.reset(startDir)
	return m, textinput.Blink
}

// ── Add-project mode ────────────────────────────────────────────────────────

func (m tuiModel) updateAddProject(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.mode = modePicker
			m.addProject.filter.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			m.addProject.moveCursor(-1)
			return m, nil
		case "down":
			m.addProject.moveCursor(1)
			return m, nil
		case "pgup":
			m.addProject.moveCursor(-10)
			return m, nil
		case "pgdown":
			m.addProject.moveCursor(10)
			return m, nil
		case "enter":
			saved, path := m.addProject.activateCursor()
			if !saved {
				return m, nil
			}
			p, err := m.db.AddProject("", path)
			if err != nil {
				m.status = "add failed: " + err.Error()
				return m, nil
			}
			m.status = addedStatus(p)
			m.mode = modePicker
			m.addProject.filter.Blur()
			return m, m.loadProjectsCmd()
		}
	}
	// Forward to the filter textinput; clamp the cursor since the visible set
	// can shrink as the user types.
	var cmd tea.Cmd
	m.addProject.filter, cmd = m.addProject.filter.Update(msg)
	m.addProject.clampCursor()
	return m, cmd
}

// ── Sessions mode ───────────────────────────────────────────────────────────

func (m tuiModel) updateSessions(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.mode = modePicker
			m.searchInput.SetValue("")
			m.searchInput.Blur()
			m.status = ""
			return m, m.loadProjectsCmd()
		case "enter":
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				s := it.s
				m.resume = &s
				return m, tea.Quit
			}
			return m, nil
		case "up", "down", "pgup", "pgdown", "home", "end":
			return m.navList(msg)
		case "ctrl+r":
			return m, m.refresh()
		case "ctrl+a":
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				return m, m.toggleArchive(it.s)
			}
			return m, nil
		case "ctrl+t":
			m.filter.ShowArchived = !m.filter.ShowArchived
			if m.filter.ShowArchived {
				m.status = "showing archived"
			} else {
				m.status = "hiding archived"
			}
			return m, m.refreshSessions()
		}
		// Other key: forward to search input. If its value changed, refilter.
		prev := m.searchInput.Value()
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		if m.searchInput.Value() != prev {
			return m, tea.Batch(cmd, m.refilterAndDisplay())
		}
		return m, cmd
	}

	// Non-key messages (mouse, ticks, etc.) — pass to viewport for scroll.
	var vpCmd tea.Cmd
	m.preview, vpCmd = m.preview.Update(msg)
	return m, vpCmd
}

// navList passes a navigation key (up/down/pgup/pgdown/home/end) to the list
// and, if the selection changed, updates the preview + dispatches an Amp fetch
// if necessary.
func (m tuiModel) navList(msg tea.Msg) (tea.Model, tea.Cmd) {
	prev := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.list.Index() != prev {
		if it, ok := m.list.SelectedItem().(sessionItem); ok {
			s := it.s
			m.preview.SetContent(renderPreview(s))
			m.preview.GotoTop()
			if needsAmpFetch(s) {
				cmd = tea.Batch(cmd, ampFetchCmd(s))
			}
		}
	}
	return m, cmd
}

func (m tuiModel) toggleArchive(s Session) tea.Cmd {
	db := m.db
	return func() tea.Msg {
		var archived int
		_ = db.sqldb.QueryRow(`SELECT archived FROM sessions WHERE id=?`, s.ID()).Scan(&archived)
		short := s.SessionUUID
		if len(short) > 8 {
			short = short[:8]
		}
		if archived == 1 {
			if err := db.Unarchive(s.ID()); err != nil {
				return archiveDoneMsg{note: "unarchive failed: " + err.Error()}
			}
			return archiveDoneMsg{note: "unarchived " + s.Tool + ":" + short}
		}
		if err := db.Archive(s.ID()); err != nil {
			return archiveDoneMsg{note: "archive failed: " + err.Error()}
		}
		return archiveDoneMsg{note: "archived " + s.Tool + ":" + short}
	}
}

func (m tuiModel) refresh() tea.Cmd {
	db := m.db
	filter := m.filter
	return func() tea.Msg {
		discovered, _ := DiscoverAll()
		_ = db.SyncSessions(discovered)
		sessions, _ := db.Query(filter)
		return refreshDoneMsg{sessions: sessions, note: fmt.Sprintf("re-scanned · %d sessions", len(sessions))}
	}
}

func (m tuiModel) refreshSessions() tea.Cmd {
	db := m.db
	filter := m.filter
	return func() tea.Msg {
		sessions, _ := db.Query(filter)
		return refreshDoneMsg{sessions: sessions}
	}
}

// softRefreshCmd is the background-tick refresh. It only re-scans file-based
// tools (no Amp network call), syncs scoped to those tools (so Amp rows aren't
// marked missing), and replies with a softRefreshDoneMsg that preserves the
// user's selection.
func (m tuiModel) softRefreshCmd() tea.Cmd {
	db := m.db
	filter := m.filter
	return func() tea.Msg {
		discovered, _ := DiscoverFileBased()
		_ = db.SyncSessionsScoped(discovered, FileBasedTools)
		sessions, _ := db.Query(filter)
		return softRefreshDoneMsg{sessions: sessions}
	}
}

// ── Apply helpers ───────────────────────────────────────────────────────────

func (m *tuiModel) applyProjects(projects []Project) {
	cwd, _ := os.Getwd()
	items := []list.Item{pickerItem{kind: pickerCurrent, cwd: cwd}}
	for _, p := range projects {
		items = append(items, pickerItem{kind: pickerSaved, project: p})
	}
	items = append(items,
		pickerItem{kind: pickerAdd},
		pickerItem{kind: pickerAll},
	)
	m.pickerList.SetItems(items)
}

// applySessions stores the unfiltered set and then renders the visible slice
// through refilterAndDisplay (which applies the current search input).
func (m *tuiModel) applySessions(sessions []Session, filter QueryFilter) tea.Cmd {
	m.allSessions = sessions
	m.filter = filter
	return m.refilterAndDisplay()
}

// refilterAndDisplay computes the search-filtered slice from m.allSessions and
// pushes it into the list, preserving the selected session's identity across
// updates (so background refreshes don't yank the cursor away).
func (m *tuiModel) refilterAndDisplay() tea.Cmd {
	visible := filterSessions(m.allSessions, m.searchInput.Value())

	prevID := ""
	if it, ok := m.list.SelectedItem().(sessionItem); ok {
		prevID = it.s.ID()
	}

	items := make([]list.Item, len(visible))
	keepIdx := -1
	for i, s := range visible {
		items[i] = sessionItem{s: s}
		if prevID != "" && s.ID() == prevID {
			keepIdx = i
		}
	}
	m.list.SetItems(items)
	m.list.Title = listTitle(m.filter, len(visible))

	var selected *Session
	switch {
	case keepIdx >= 0:
		m.list.Select(keepIdx)
		s := visible[keepIdx]
		selected = &s
	case len(visible) > 0:
		m.list.Select(0)
		m.preview.SetContent(renderPreview(visible[0]))
		m.preview.GotoTop()
		s := visible[0]
		selected = &s
	default:
		if m.searchInput.Value() != "" {
			m.preview.SetContent("(no matches)")
		} else {
			m.preview.SetContent("(no sessions in this scope)")
		}
		m.preview.GotoTop()
	}

	if selected != nil && needsAmpFetch(*selected) {
		return ampFetchCmd(*selected)
	}
	return nil
}

// filterSessions returns the fuzzy-matched subset of all in score order.
// Matching is over "title cwd tool first-message" concatenated per session.
func filterSessions(all []Session, query string) []Session {
	q := strings.TrimSpace(query)
	if q == "" {
		return all
	}
	items := make([]string, len(all))
	for i, s := range all {
		items[i] = s.Title + " " + s.CWD + " " + s.Tool + " " + s.FirstMsg
	}
	matches := fuzzy.Find(q, items)
	out := make([]Session, len(matches))
	for i, mm := range matches {
		out[i] = all[mm.Index]
	}
	return out
}

func listTitle(f QueryFilter, n int) string {
	scope := "all cwds"
	if f.CWDPrefix != "" {
		scope = f.CWDPrefix
	}
	return fmt.Sprintf("Sessions · %d · %s", n, scope)
}

// ── View dispatch ───────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	if !m.ready {
		return "loading…"
	}
	switch m.mode {
	case modePicker:
		return m.viewPicker()
	case modeAddProject:
		return m.viewAddProject()
	case modeSessions:
		return m.viewSessions()
	}
	return ""
}

func (m tuiModel) viewPicker() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("240"))
	body := border.Render(m.pickerList.View())
	keys := "↑/↓ select · enter open · + add · / filter · q quit"
	return lipgloss.JoinVertical(lipgloss.Left, body, statusLine(m.status, keys))
}

func (m tuiModel) viewAddProject() string {
	return m.addProject.view(m.width, m.height)
}

func (m tuiModel) viewSessions() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("240"))
	inputRow := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		BorderForeground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).
		Width(m.width - 2).
		Render(m.searchInput.View())
	left := border.Render(m.list.View())
	right := border.Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	keys := "type to search · ↑/↓ move · enter resume · ctrl+r refresh · ctrl+a archive · ctrl+t toggle archived · esc back"
	return lipgloss.JoinVertical(lipgloss.Left, inputRow, body, statusLine(m.status, keys))
}

// statusLine renders the bottom hint row with success-green / error-red
// coloring for the transient status when present.
func statusLine(status, keys string) string {
	faint := lipgloss.NewStyle().Faint(true)
	if status == "" {
		return faint.Render(keys)
	}
	var s lipgloss.Style
	switch {
	case strings.Contains(status, "failed"), strings.Contains(status, "error"):
		s = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	default:
		s = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
	}
	return s.Render(status) + faint.Render(" · "+keys)
}

// ── Layout ──────────────────────────────────────────────────────────────────

func (m *tuiModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Picker view: just list + 1-line help.
	pickerBodyH := m.height - 2
	pad := 4
	m.pickerList.SetSize(max1(m.width-pad, 20), max1(pickerBodyH-2, 6))

	// Sessions view: search input (3 lines incl. border) + body + 1-line help.
	sessionBodyH := m.height - 5
	leftW := m.width / 2
	rightW := m.width - leftW
	m.list.SetSize(max1(leftW-pad, 10), max1(sessionBodyH-2, 6))
	m.preview.Width = max1(rightW-pad, 10)
	m.preview.Height = max1(sessionBodyH-2, 6)
	m.searchInput.Width = max1(m.width-10, 20)
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Entry point ─────────────────────────────────────────────────────────────

func runTUI(db *DB) (*Session, error) {
	m := newTUI(db)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	fm := final.(tuiModel)
	return fm.resume, nil
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	default:
		return t.Format("2006-01-02")
	}
}
