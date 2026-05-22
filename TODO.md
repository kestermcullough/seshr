# TODO

Living roadmap. Top of the list is "do next."

---

## Project picker — file-tree modal for "+ Add"

Replace the bare text input behind "+ Add project" with a centered popup that lets you browse the filesystem.

- Centered modal (~60% of viewport, lipgloss.Place to position)
- Tree navigation (↑/↓ ← → or h/j/k/l)
- Type to fast-find (substring/fuzzy filter on visible entries)
- Enter on a directory → save as project
- Esc → cancel

`bubbles/filepicker` handles tree navigation; layer a `textinput` on top for the type-to-filter, or roll our own with `list.Model` over `os.ReadDir`. Filepicker route is the shorter one — start there and add filter as a wrap.

## Sessions screen — more color

Tool-name chips landed already (orange/teal/purple/pink for claude/codex/amp/pi). Still to do:

- Scope chip in the list title (`/home/kester/mainframe`) styled distinctly
- Archived indicator on rows (dim text + tag)
- Missing indicator on rows (red strike-through or marker)
- Status line: success green, error red
- Selection highlight that respects the tool color of the selected row

## Project picker — polish (smaller tasks)

- Manual reorder (`J`/`K` to push pinned rows up/down → writes sort_order)
- Rename project (`r`)
- Remove project (`d` with confirmation)
- "Save as project?" prompt when entering Current dir with no saved project

---

## Earlier-conceived major work: project-based navigation

Replace the flat list-with-cwd-filter UX with a two-level model:

1. **Project picker** (first screen on launch)
2. **Session list** (the existing two-pane TUI, scoped to the chosen project)

### Picker layout

```
┌─ Projects ────────────────────────────────┐
│ ▶ Current dir   (/home/kester/foo)        │
│   mainframe     /home/kester/mainframe    │  ← user-added, sortable
│   cl2-ctrl      /mnt/c/.../cl2 controller │
│   markdownii    /mnt/c/.../markdownii     │
│   …                                        │
│   + Add project                            │
│   All sessions                             │
└────────────────────────────────────────────┘
```

- **Current dir**: ephemeral, always at the top. Picks up `os.Getwd()` on launch. If the user visits a session under it, prompt to save as a project.
- **User-added projects**: sorted by `last_used_at desc` by default; manual reorder (k/J on the row, or future drag) writes a non-NULL `sort_order` that pins position until moved again. New projects appear by recency until pinned.
- **+ Add project**: opens a sub-screen showing *discovered cwds* (every cwd we've seen in any session) ranked by session count + recency. One-click to promote. Also accepts arbitrary paths typed in.
- **All sessions**: always at the bottom. Same flat view we have today.

### Subproject behavior

Recursive prefix match (current behavior). A session under `/home/kester/mainframe` shows in both a `/home/kester` project view and a `/home/kester/mainframe` project view — they're different lenses, both valid. No dedup. Document so it isn't surprising.

### Schema

New table:

```sql
CREATE TABLE projects (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,         -- display; defaults to filepath.Base(path)
  path         TEXT NOT NULL UNIQUE,  -- the cwd prefix used for filtering
  sort_order   INTEGER,               -- NULL = sort by last_used_at; non-NULL pins
  last_used_at INTEGER,
  added_at     INTEGER NOT NULL
);
```

`last_used_at` updates whenever the user enters that project's session view.

### Implementation pieces

- `projects.go` — CRUD on the new table (add, rename, remove, reorder, bump-last-used)
- TUI: new picker screen as the entry view; existing session view becomes the second screen pushed onto a tiny screen stack
- Discovered-cwds suggestion query: `SELECT cwd, COUNT(*), MAX(last_active) FROM sessions WHERE cwd != '' GROUP BY cwd ORDER BY 3 DESC`
- Back nav from session view → picker (Esc when filter is closed)
- Persist last-opened project so re-launch goes straight there? (open question — initial vote: no, always show picker; user can `q` then re-enter)

---

## Other priorities (mostly already agreed)

1. **Always-on search bar.** Right now `/` enters filter mode (modal). The brief was *as you type* — input should always be focused at the top of the list, no modal step. Refactor the TUI to drive `list.Model` from an external `textinput.Model` instead of using its built-in filter.

2. **Real Amp previews.** 44 of 151 sessions show metadata only because Amp content lives at ampcode.com. Shell out to `amp threads export <id>` lazily (first time the preview is opened) and cache the result. Cache target: a `preview_cache` column on `sessions` or a sidecar dir under `~/.local/share/agent-sessions/cache/`.

3. **End-to-end resume test.** `syscall.Exec` paths are written for all four tools but none have actually been triggered. Pick a throwaway session of each kind and verify the handoff feels clean (terminal state restored, alt-screen exited, parent process gone).

4. **Claude cwd accuracy.** Today: slug-decode `/mnt/c/.../OneDrive - HRSD/...` becomes `OneDrive///HRSD/...`. Two-line fix: when parsing Claude JSONL, also check for a per-record `cwd` field (present in some Claude record types) and prefer it over the slug-decoded path. Also unblocks accurate cwd-prefix filtering for projects on OneDrive paths.

5. **Polish that'll matter after a week of use:**
   - Archived indicator in the list (so `--archived` view is readable)
   - Recently-opened sort boost (opened in last hour → bump up)
   - Double-click on a row → resume
   - Tool color in the list (claude/codex/amp/pi each get a hue)

6. **Name the tool.** `agent-sessions` is a placeholder. Candidates to noodle on: `agenda`, `threadwise`, `sessions`, `recall`, `mux`. (Open to anything.)

---

## Deferred / nice-to-have

- **Content search (FTS5).** Title + cwd is enough for v1; full-text over messages is a real feature but a separate effort (lazy indexing, ranked snippets, etc.).
- **Codex title quality.** Some Codex sessions start with pasted terminal output, which becomes a noisy "title." Could filter out lines starting with `kester@...$` or detect shell-prompt patterns.
- **Cross-tool actions.** Replay a Codex session in Claude, etc. Out of scope.
- **Multi-machine / sync.** If you ever want this on multiple machines, the DB becomes a sync problem. Punt.
- **Auth tokens.** None of this needs auth except Amp, and Amp's CLI handles its own.
- **Performance.** Currently re-parses every session file on each launch. With ~150 sessions it's ~1s; if it grows past ~1000 we'd want mtime-skip on the upsert path.
