package main

import (
	"fmt"
	"os"
	"sort"
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
	modeInfo
)

// ── Picker prompts ──────────────────────────────────────────────────────────

type pickerPromptKind int

const (
	promptNone pickerPromptKind = iota
	promptRename
	promptDeleteConfirm
)

type pickerPromptState struct {
	kind   pickerPromptKind
	target Project
	input  textinput.Model
}

// ── Picker items ────────────────────────────────────────────────────────────

type pickerKind int

const (
	pickerCurrent pickerKind = iota
	pickerSaved
	pickerSpacer
	pickerAdd
	pickerAll
)

type pickerItem struct {
	kind    pickerKind
	project Project // valid when kind == pickerSaved
	cwd     string  // valid when kind == pickerCurrent
}

// Style chips for synthetic picker rows so the user-added projects read as
// the primary content and the action/sentinel rows recede.
var (
	pickerDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"})
	pickerActionStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).Italic(true)
	pickerSavedStyle  = lipgloss.NewStyle().Bold(true)
)

func (i pickerItem) Title() string {
	switch i.kind {
	case pickerCurrent:
		return pickerDimStyle.Render("● Current dir")
	case pickerSaved:
		return pickerSavedStyle.Render(i.project.Name)
	case pickerSpacer:
		return ""
	case pickerAdd:
		return pickerActionStyle.Render("+ Add project")
	case pickerAll:
		return pickerActionStyle.Render("○ All sessions")
	}
	return "?"
}

func (i pickerItem) Description() string {
	switch i.kind {
	case pickerCurrent:
		if i.cwd == "" {
			return pickerDimStyle.Render("(could not detect cwd)")
		}
		return pickerDimStyle.Render(i.cwd)
	case pickerSaved:
		return i.project.Path
	case pickerSpacer:
		return ""
	case pickerAdd:
		return pickerDimStyle.Render("save the current dir or another path as a project")
	case pickerAll:
		return pickerDimStyle.Render("every session across every cwd")
	}
	return ""
}

func (i pickerItem) FilterValue() string {
	if i.kind == pickerSpacer {
		return "" // exclude from filter results
	}
	return i.Title() + " " + i.Description()
}

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

type sessionItem struct {
	s      Session
	tokens []string // active search tokens, used to highlight matches inline
}

func (i sessionItem) Title() string {
	t := i.s.Title
	if t == "" {
		t = "(no title)"
	}
	return renderToolLabel(i.s.Tool) + " " + highlightTokens(t, i.tokens)
}

// Badge styles for archived/missing/live markers in the row description.
var (
	archivedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).
			Italic(true).
			Render("[archived]")
	missingBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"}).
			Render("[missing]")
	liveBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"}).
			Bold(true).
			Render("● live")
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
	var parts []string
	if i.s.Live {
		parts = append(parts, liveBadge)
	}
	if i.s.Missing {
		parts = append(parts, missingBadge)
	} else if i.s.Archived {
		parts = append(parts, archivedBadge)
	}
	parts = append(parts, when, highlightTokens(cwd, i.tokens))
	return strings.Join(parts, "  ")
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

	// inline picker prompts (rename / delete confirm)
	prompt pickerPromptState

	// pendingFocusProjectID, when non-zero, makes the next applyProjects
	// move the picker cursor onto that project (used for J/K reorder so
	// the cursor follows the moved row).
	pendingFocusProjectID int64

	// cleanupWarning, when non-empty, is rendered at the bottom-right of
	// the picker as a subtle reminder that one of the agents is
	// auto-pruning sessions. Populated once at TUI launch (cheap to
	// refresh later if we ever want it live).
	cleanupWarning string

	// cacheStats is seshr's own disk usage (DB + Amp content cache),
	// refreshed each time projects are loaded so the bottom-bar number
	// stays roughly in sync as the Amp cache grows.
	cacheStats CacheStats

	// agentStorage is computed on demand when the info modal opens.
	agentStorage []AgentStorageStat

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
	pickerDelegate := list.NewDefaultDelegate()
	// Unset the delegate's title/desc foreground so per-item ANSI styling
	// in pickerItem.Title/Description survives (dim/italic for synthetic
	// rows, bold for saved projects).
	pickerDelegate.Styles.NormalTitle = pickerDelegate.Styles.NormalTitle.UnsetForeground()
	pickerDelegate.Styles.NormalDesc = pickerDelegate.Styles.NormalDesc.UnsetForeground()
	pl := list.New(nil, pickerDelegate, 0, 0)
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

	promptInput := textinput.New()
	promptInput.CharLimit = 128

	m := tuiModel{
		db:          db,
		mode:        modePicker,
		pickerList:  pl,
		list:        sl,
		preview:     vp,
		searchInput: si,
		addProject:  newAddProject(db),
		prompt:      pickerPromptState{input: promptInput},
	}
	m.cleanupWarning = computeCleanupWarning()
	m.cacheStats = seshrCacheStats()
	return m
}

// computeCleanupWarning returns the short text shown at the picker's
// bottom-right corner when any agent has aggressive auto-pruning enabled.
// Empty string means "nothing to warn about, don't render."
func computeCleanupWarning() string {
	days, _, err := ClaudeCleanupPeriodDays()
	if err != nil || days <= 0 || days >= claudeCleanupWarnThreshold {
		return ""
	}
	return fmt.Sprintf("⚠ claude auto-prunes after %dd · seshr settings claude-cleanup-days 365", days)
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
		m.cacheStats = seshrCacheStats() // keep the picker's footer in sync
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
				m.preview.SetContent(renderPreview(it.s, m.currentTokens()))
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
	case modeInfo:
		return m.updateInfo(msg)
	}
	return m, nil
}

// updateInfo handles the disk-usage modal: any of esc/i/q/ctrl+c dismisses it.
func (m tuiModel) updateInfo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "q", "i":
			m.mode = modePicker
			return m, nil
		}
	}
	return m, nil
}

// ── Picker mode ─────────────────────────────────────────────────────────────

func (m tuiModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Inline prompts (rename input, delete confirm) consume input first.
	if m.prompt.kind != promptNone {
		return m.updatePickerPrompt(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.pickerList.FilterState() == list.Filtering {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			break
		}
		sel, selOK := m.pickerList.SelectedItem().(pickerItem)
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "enter":
			if selOK {
				if sel.kind != pickerAdd {
					m.status = "scanning…"
				}
				return m.handlePickerEnter(sel)
			}
		case "+":
			return m.enterAddMode("")
		case "r":
			if selOK && sel.kind == pickerSaved {
				m.prompt.kind = promptRename
				m.prompt.target = sel.project
				m.prompt.input.SetValue(sel.project.Name)
				m.prompt.input.CursorEnd()
				m.prompt.input.Focus()
				return m, textinput.Blink
			}
		case "d":
			if selOK && sel.kind == pickerSaved {
				m.prompt.kind = promptDeleteConfirm
				m.prompt.target = sel.project
				return m, nil
			}
		case "J":
			if selOK && sel.kind == pickerSaved {
				if err := m.db.MoveProjectDown(sel.project.ID); err != nil {
					m.status = "move failed: " + err.Error()
				} else {
					m.status = "moved down"
					m.pendingFocusProjectID = sel.project.ID
				}
				return m, m.loadProjectsCmd()
			}
		case "K":
			if selOK && sel.kind == pickerSaved {
				if err := m.db.MoveProjectUp(sel.project.ID); err != nil {
					m.status = "move failed: " + err.Error()
				} else {
					m.status = "moved up"
					m.pendingFocusProjectID = sel.project.ID
				}
				return m, m.loadProjectsCmd()
			}
		case "i":
			// Compute agent-storage stats on demand (walks tool dirs once).
			m.agentStorage = agentStorageStats()
			m.cacheStats = seshrCacheStats()
			m.mode = modeInfo
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.pickerList, cmd = m.pickerList.Update(msg)
	return m, cmd
}

func (m tuiModel) updatePickerPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.prompt.kind {
	case promptRename:
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.prompt.kind = promptNone
			m.prompt.input.Blur()
			return m, nil
		case "enter":
			newName := strings.TrimSpace(m.prompt.input.Value())
			if newName == "" {
				m.status = "name cannot be empty"
				return m, nil
			}
			if err := m.db.RenameProject(m.prompt.target.ID, newName); err != nil {
				m.status = "rename failed: " + err.Error()
				return m, nil
			}
			m.status = "renamed to " + newName
			m.prompt.kind = promptNone
			m.prompt.input.Blur()
			return m, m.loadProjectsCmd()
		}
		var cmd tea.Cmd
		m.prompt.input, cmd = m.prompt.input.Update(msg)
		return m, cmd

	case promptDeleteConfirm:
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "y", "Y":
			name := m.prompt.target.Name
			if err := m.db.RemoveProject(m.prompt.target.ID); err != nil {
				m.status = "delete failed: " + err.Error()
				m.prompt.kind = promptNone
				return m, nil
			}
			m.status = "removed " + name
			m.prompt.kind = promptNone
			return m, m.loadProjectsCmd()
		case "n", "N", "esc":
			m.prompt.kind = promptNone
			m.status = ""
			return m, nil
		}
	}
	return m, nil
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
	case pickerSpacer:
		// Visual divider; Enter is a no-op.
		return m, nil
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
	// Mouse events: route by horizontal position. Wheel/click on the left
	// half goes to the list (cursor/scroll), right half goes to the
	// preview viewport.
	if mm, ok := msg.(tea.MouseMsg); ok {
		var cmd tea.Cmd
		if mm.X < m.width/2 {
			prev := m.list.Index()
			m.list, cmd = m.list.Update(mm)
			if m.list.Index() != prev {
				if it, ok := m.list.SelectedItem().(sessionItem); ok {
					s := it.s
					m.preview.SetContent(renderPreview(s, m.currentTokens()))
					m.preview.GotoTop()
					if needsAmpFetch(s) {
						cmd = tea.Batch(cmd, ampFetchCmd(s))
					}
				}
			}
		} else {
			m.preview, cmd = m.preview.Update(mm)
		}
		return m, cmd
	}

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
			m.preview.SetContent(renderPreview(s, m.currentTokens()))
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
	focusIdx := -1
	for _, p := range projects {
		idx := len(items)
		items = append(items, pickerItem{kind: pickerSaved, project: p})
		if m.pendingFocusProjectID != 0 && p.ID == m.pendingFocusProjectID {
			focusIdx = idx
		}
	}
	// A blank row visually separates saved projects from the action rows below.
	if len(projects) > 0 {
		items = append(items, pickerItem{kind: pickerSpacer})
	}
	items = append(items,
		pickerItem{kind: pickerAdd},
		pickerItem{kind: pickerAll},
	)
	m.pickerList.SetItems(items)
	if focusIdx >= 0 {
		m.pickerList.Select(focusIdx)
	}
	m.pendingFocusProjectID = 0
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

	tokens := tokenize(m.searchInput.Value())
	items := make([]list.Item, len(visible))
	keepIdx := -1
	for i, s := range visible {
		items[i] = sessionItem{s: s, tokens: tokens}
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
		// Refresh preview to update transcript-match highlights as the user types.
		m.preview.SetContent(renderPreview(s, tokens))
	case len(visible) > 0:
		m.list.Select(0)
		m.preview.SetContent(renderPreview(visible[0], tokens))
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

// currentTokens returns the active search tokens (lowercased, whitespace-split)
// so callers can plumb them into renderPreview / highlight helpers.
func (m tuiModel) currentTokens() []string {
	return tokenize(m.searchInput.Value())
}

// tokenize splits the search query on whitespace and lowercases the result.
// An empty/whitespace query yields nil.
func tokenize(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	parts := strings.Fields(q)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.ToLower(p))
	}
	return out
}

// filterSessions returns sessions that contain *every* search token as a
// case-insensitive substring of (title cwd tool first-message), ranked by the
// earliest token-match position. This is tighter than fuzzy character-skip
// matching, which was returning too many irrelevant hits.
func filterSessions(all []Session, query string) []Session {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return all
	}
	type scored struct {
		s   Session
		pos int
	}
	var matches []scored
	for _, s := range all {
		hay := strings.ToLower(s.Title + " " + s.CWD + " " + s.Tool + " " + s.FirstMsg)
		minPos := -1
		ok := true
		for _, t := range tokens {
			idx := strings.Index(hay, t)
			if idx < 0 {
				ok = false
				break
			}
			if minPos < 0 || idx < minPos {
				minPos = idx
			}
		}
		if ok {
			matches = append(matches, scored{s: s, pos: minPos})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pos < matches[j].pos })
	out := make([]Session, len(matches))
	for i, m := range matches {
		out[i] = m.s
	}
	return out
}

// highlightTokens wraps every case-insensitive occurrence of each token in
// text with an underlined, bold, orange style. Overlapping ranges are merged
// so the output is well-formed.
var matchHighlightStyle = lipgloss.NewStyle().
	Underline(true).
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"})

func highlightTokens(text string, tokens []string) string {
	if text == "" || len(tokens) == 0 {
		return text
	}
	type rng struct{ s, e int }
	var ranges []rng
	lo := strings.ToLower(text)
	for _, t := range tokens {
		if t == "" {
			continue
		}
		start := 0
		for {
			idx := strings.Index(lo[start:], t)
			if idx < 0 {
				break
			}
			abs := start + idx
			ranges = append(ranges, rng{abs, abs + len(t)})
			start = abs + len(t)
		}
	}
	if len(ranges) == 0 {
		return text
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].s < ranges[j].s })
	merged := []rng{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.s <= last.e {
			if r.e > last.e {
				last.e = r.e
			}
		} else {
			merged = append(merged, r)
		}
	}
	var sb strings.Builder
	prev := 0
	for _, r := range merged {
		sb.WriteString(text[prev:r.s])
		sb.WriteString(matchHighlightStyle.Render(text[r.s:r.e]))
		prev = r.e
	}
	sb.WriteString(text[prev:])
	return sb.String()
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
	case modeInfo:
		return m.viewInfo()
	}
	return ""
}

func (m tuiModel) viewPicker() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("240"))
	body := border.Render(m.pickerList.View())

	if m.prompt.kind != promptNone {
		var promptLine string
		switch m.prompt.kind {
		case promptRename:
			label := lipgloss.NewStyle().Bold(true).Render("rename → ")
			promptLine = label + m.prompt.input.View() +
				lipgloss.NewStyle().Faint(true).Render("    enter save · esc cancel")
		case promptDeleteConfirm:
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"}).Bold(true)
			promptLine = redStyle.Render(
				fmt.Sprintf("remove project '%s'?", m.prompt.target.Name)) +
				lipgloss.NewStyle().Faint(true).Render("    y confirm · n/esc cancel")
		}
		return lipgloss.JoinVertical(lipgloss.Left, body, promptLine)
	}

	keys := "↑/↓ select · enter open · + add · r rename · d remove · J/K reorder · i info · / filter · q quit"
	bottom := statusLine(m.status, keys)
	right := m.bottomRightChips()
	if right != "" && m.width > 0 {
		gap := m.width - lipgloss.Width(bottom) - lipgloss.Width(right)
		if gap >= 1 {
			bottom = bottom + strings.Repeat(" ", gap) + right
		} else {
			bottom = bottom + "\n" + right
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, bottom)
}

// bottomRightChips renders the right side of the picker's footer:
// a faint "cache N" total, followed by the cleanup warning if active.
func (m tuiModel) bottomRightChips() string {
	faint := lipgloss.NewStyle().Faint(true)
	var parts []string
	if m.cacheStats.TotalBytes > 0 {
		parts = append(parts, faint.Render(fmt.Sprintf("cache %s", humanSize(m.cacheStats.TotalBytes))))
	}
	if m.cleanupWarning != "" {
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"}).
			Render(m.cleanupWarning)
		parts = append(parts, warn)
	}
	return strings.Join(parts, faint.Render(" · "))
}

func (m tuiModel) viewInfo() string {
	header := lipgloss.NewStyle().Bold(true).Render("Disk usage")
	faint := lipgloss.NewStyle().Faint(true)
	subHead := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "36"})

	var sb strings.Builder
	sb.WriteString(header + "\n\n")

	sb.WriteString(subHead.Render("seshr local cache") + "\n")
	sb.WriteString(fmt.Sprintf("  metadata DB        %s\n", humanSize(m.cacheStats.DBBytes)))
	sb.WriteString(fmt.Sprintf("  amp content cache  %s  %s\n",
		humanSize(m.cacheStats.AmpCacheBytes),
		faint.Render(fmt.Sprintf("(%d threads cached)", m.cacheStats.AmpCacheFiles))))
	sb.WriteString(faint.Render("  ────────────────────────\n"))
	sb.WriteString(fmt.Sprintf("  total              %s\n\n", humanSize(m.cacheStats.TotalBytes)))

	sb.WriteString(subHead.Render("agent session storage") + faint.Render("   (managed by each tool)") + "\n")
	for _, a := range m.agentStorage {
		tool := renderToolLabel(a.Tool)
		sb.WriteString(fmt.Sprintf("  %s %10s  %s\n",
			tool, humanSize(a.Bytes),
			faint.Render(fmt.Sprintf("%d files at %s", a.FileCount, a.Path))))
	}
	sb.WriteString("\n")
	sb.WriteString(faint.Render("esc/i/q  close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		BorderForeground(lipgloss.AdaptiveColor{Light: "240", Dark: "245"}).
		Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
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
