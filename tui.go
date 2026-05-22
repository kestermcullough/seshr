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

// ── Session items (unchanged shape) ─────────────────────────────────────────

type sessionItem struct{ s Session }

func (i sessionItem) Title() string {
	tool := strings.ToUpper(i.s.Tool)
	t := i.s.Title
	if t == "" {
		t = "(no title)"
	}
	return fmt.Sprintf("[%-6s] %s", tool, t)
}

func (i sessionItem) Description() string {
	when := "—"
	if !i.s.LastActive.IsZero() {
		when = humanTime(i.s.LastActive)
	}
	cwd := i.s.CWD
	if cwd == "" {
		cwd = "?"
	}
	return when + "  " + cwd
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

	// add-project input
	addInput textinput.Model

	// sessions
	list    list.Model
	preview viewport.Model
	filter  QueryFilter

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

	sl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	sl.Title = "Sessions"
	sl.SetShowStatusBar(true)
	sl.SetFilteringEnabled(true)
	sl.SetShowHelp(true)

	vp := viewport.New(0, 0)
	vp.SetContent("(select a session to preview)")

	in := textinput.New()
	in.Placeholder = "path"
	in.CharLimit = 1024

	return tuiModel{
		db:         db,
		mode:       modePicker,
		pickerList: pl,
		list:       sl,
		preview:    vp,
		addInput:   in,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.loadProjectsCmd(), textinput.Blink)
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
		m.applySessions(msg.sessions, msg.filter)
		m.mode = modeSessions
		return m, nil

	case archiveDoneMsg:
		m.status = msg.note
		return m, m.refreshSessions()

	case refreshDoneMsg:
		m.applySessions(msg.sessions, m.filter)
		if msg.note != "" {
			m.status = msg.note
		}
		return m, nil
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
		return m, m.loadSessionsCmd(QueryFilter{CWDPrefix: it.project.Path}, it.project.ID)
	case pickerAdd:
		cwd, _ := os.Getwd()
		return m.enterAddMode(cwd)
	case pickerAll:
		return m, m.loadSessionsCmd(QueryFilter{}, 0)
	}
	return m, nil
}

func (m tuiModel) enterAddMode(prefill string) (tea.Model, tea.Cmd) {
	m.mode = modeAddProject
	m.addInput.SetValue(prefill)
	m.addInput.Focus()
	return m, textinput.Blink
}

// ── Add-project mode ────────────────────────────────────────────────────────

func (m tuiModel) updateAddProject(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.mode = modePicker
			m.addInput.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			path := strings.TrimSpace(m.addInput.Value())
			if path == "" {
				m.status = "path required"
				return m, nil
			}
			p, err := m.db.AddProject("", path)
			if err != nil {
				m.status = "add failed: " + err.Error()
				return m, nil
			}
			m.status = "added: " + p.Name
			m.mode = modePicker
			m.addInput.Blur()
			return m, m.loadProjectsCmd()
		}
	}
	var cmd tea.Cmd
	m.addInput, cmd = m.addInput.Update(msg)
	return m, cmd
}

// ── Sessions mode ───────────────────────────────────────────────────────────

func (m tuiModel) updateSessions(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if m.list.FilterState() == list.Filtering {
			if km.String() == "ctrl+c" {
				return m, tea.Quit
			}
			// fall through to list.Update
		} else {
			switch km.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc":
				// Back to picker
				m.mode = modePicker
				m.status = ""
				return m, m.loadProjectsCmd()
			case "enter":
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					s := it.s
					m.resume = &s
					return m, tea.Quit
				}
			case "a":
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					return m, m.toggleArchive(it.s)
				}
			case "R":
				return m, m.refresh()
			}
		}
	}

	prev := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.list.Index() != prev {
		if it, ok := m.list.SelectedItem().(sessionItem); ok {
			m.preview.SetContent(renderPreview(it.s))
			m.preview.GotoTop()
		}
	}
	var vpCmd tea.Cmd
	m.preview, vpCmd = m.preview.Update(msg)
	return m, tea.Batch(cmd, vpCmd)
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

func (m *tuiModel) applySessions(sessions []Session, filter QueryFilter) {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s: s}
	}
	m.list.SetItems(items)
	m.list.Title = listTitle(filter, len(sessions))
	m.filter = filter
	if len(sessions) > 0 {
		m.preview.SetContent(renderPreview(sessions[0]))
	} else {
		m.preview.SetContent("(no sessions in this scope)")
	}
	m.preview.GotoTop()
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
	help := "↑/↓ select · enter open · + add · / filter · q quit"
	if m.status != "" {
		help = m.status + " · " + help
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, lipgloss.NewStyle().Faint(true).Render(help))
}

func (m tuiModel) viewAddProject() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(lipgloss.Color("240"))
	body := border.Render(lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render("Add project"),
		"",
		"Path (e.g. /home/kester/mainframe):",
		m.addInput.View(),
	))
	help := "enter save · esc cancel"
	return lipgloss.JoinVertical(lipgloss.Left, body, lipgloss.NewStyle().Faint(true).Render(help))
}

func (m tuiModel) viewSessions() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("240"))
	left := border.Render(m.list.View())
	right := border.Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	help := "j/k move · / filter · enter resume · a archive · R refresh · esc back · q quit"
	if m.status != "" {
		help = m.status + " · " + help
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, lipgloss.NewStyle().Faint(true).Render(help))
}

// ── Layout ──────────────────────────────────────────────────────────────────

func (m *tuiModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	bodyH := m.height - 2
	pad := 4

	m.pickerList.SetSize(max1(m.width-pad, 20), max1(bodyH-2, 6))

	leftW := m.width / 2
	rightW := m.width - leftW
	m.list.SetSize(max1(leftW-pad, 10), max1(bodyH-2, 6))
	m.preview.Width = max1(rightW-pad, 10)
	m.preview.Height = max1(bodyH-2, 6)

	m.addInput.Width = max1(m.width-12, 20)
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
