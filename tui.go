package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sessionItem adapts a Session into bubbles/list's Item interface.
// FilterValue intentionally bundles title + cwd + tool + first_msg so the
// list's built-in fuzzy filter (press `/`) feels "smart" without us writing
// our own search.
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

type tuiModel struct {
	db       *DB
	filter   QueryFilter
	list     list.Model
	preview  viewport.Model
	width    int
	height   int
	ready    bool
	status   string
	selected *Session // currently highlighted session
	resume   *Session // set on Enter; main acts on it after TUI quits
}

func newTUI(db *DB, sessions []Session, filter QueryFilter) tuiModel {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s: s}
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = listTitle(filter, len(sessions))
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	vp := viewport.New(0, 0)
	vp.SetContent("(select a session to preview)")

	m := tuiModel{db: db, filter: filter, list: l, preview: vp}
	if len(sessions) > 0 {
		s := sessions[0]
		m.selected = &s
		m.preview.SetContent(renderPreview(s))
	}
	return m
}

func listTitle(f QueryFilter, n int) string {
	scope := "all cwds"
	if f.CWDPrefix != "" {
		scope = f.CWDPrefix
	}
	return fmt.Sprintf("Agent Sessions · %d · %s", n, scope)
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		// When the list's filter input has focus, let the list handle everything
		// except plain Ctrl+C so users can type `a`, `q`, etc. as filter text.
		if m.list.FilterState() == list.Filtering {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			break
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
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
	case archiveDoneMsg:
		m.status = msg.note
		// Refresh from DB to reflect the change.
		return m, m.refresh()
	case refreshDoneMsg:
		m.applySessions(msg.sessions)
		m.status = fmt.Sprintf("refreshed · %d sessions", len(msg.sessions))
		return m, nil
	}

	var cmd tea.Cmd
	prev := m.list.Index()
	m.list, cmd = m.list.Update(msg)
	// If selection changed, regenerate preview.
	if m.list.Index() != prev {
		if it, ok := m.list.SelectedItem().(sessionItem); ok {
			s := it.s
			m.selected = &s
			m.preview.SetContent(renderPreview(s))
			m.preview.GotoTop()
		}
	}
	var vpCmd tea.Cmd
	m.preview, vpCmd = m.preview.Update(msg)
	return m, tea.Batch(cmd, vpCmd)
}

func (m tuiModel) View() string {
	if !m.ready {
		return "loading…"
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		BorderForeground(lipgloss.Color("240"))

	left := border.Render(m.list.View())
	right := border.Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	help := "j/k move · / filter · enter resume · a archive · R refresh · q quit"
	if m.status != "" {
		help = m.status + " · " + help
	}
	helpLine := lipgloss.NewStyle().Faint(true).Render(help)
	return lipgloss.JoinVertical(lipgloss.Left, body, helpLine)
}

func (m *tuiModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Reserve 2 lines (help line + spacing) and ~4 chars for borders/padding.
	bodyH := m.height - 2
	leftW := m.width / 2
	rightW := m.width - leftW
	pad := 4 // border (2) + padding (2)

	m.list.SetSize(max1(leftW-pad, 10), max1(bodyH-2, 6))
	m.preview.Width = max1(rightW-pad, 10)
	m.preview.Height = max1(bodyH-2, 6)
}

func max1(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Archive/refresh use tea.Cmd-returning helpers so DB I/O happens off the UI
// goroutine.
type archiveDoneMsg struct{ note string }
type refreshDoneMsg struct{ sessions []Session }

func (m *tuiModel) toggleArchive(s Session) tea.Cmd {
	return func() tea.Msg {
		var note string
		// Look up the current archived state from the DB to keep things simple.
		// (We don't track it on the in-memory Session right now.)
		var archived int
		_ = m.db.sqldb.QueryRow(`SELECT archived FROM sessions WHERE id=?`, s.ID()).Scan(&archived)
		if archived == 1 {
			if err := m.db.Unarchive(s.ID()); err != nil {
				return archiveDoneMsg{note: "unarchive failed: " + err.Error()}
			}
			note = "unarchived"
		} else {
			if err := m.db.Archive(s.ID()); err != nil {
				return archiveDoneMsg{note: "archive failed: " + err.Error()}
			}
			note = "archived"
		}
		return archiveDoneMsg{note: note + " " + s.Tool + ":" + s.SessionUUID[:8]}
	}
}

func (m *tuiModel) refresh() tea.Cmd {
	db := m.db
	filter := m.filter
	return func() tea.Msg {
		discovered, _ := DiscoverAll()
		_ = db.SyncSessions(discovered)
		sessions, _ := db.Query(filter)
		return refreshDoneMsg{sessions: sessions}
	}
}

func (m *tuiModel) applySessions(sessions []Session) {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s: s}
	}
	m.list.SetItems(items)
	m.list.Title = listTitle(m.filter, len(sessions))
	if len(sessions) > 0 {
		s := sessions[0]
		m.selected = &s
		m.preview.SetContent(renderPreview(s))
	} else {
		m.selected = nil
		m.preview.SetContent("(no sessions)")
	}
}

// runTUI is the entry point from main. It returns the session the user chose
// to resume (or nil if they quit without selecting).
func runTUI(db *DB, sessions []Session, filter QueryFilter) (*Session, error) {
	m := newTUI(db, sessions, filter)
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
