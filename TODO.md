# TODO

Living roadmap. Top of the list is "do next."

---

## 1. Always-on search bar (sessions view)

The brief was *"fully searchable fast smart search as you type"* — today `/` enters a modal filter mode, which isn't that. Refactor: an always-focused `textinput.Model` at the top of the sessions screen drives filtering directly. List items are filtered in our code (fuzzy match via `sahilm/fuzzy`, which we already depend on) and fed into `list.SetItems`.

Side benefit: bubbles/list's built-in filter overlay goes away, freeing the `/` key for something else.

## 2. Project picker — polish

- Manual reorder (`J`/`K` to push pinned rows up/down → writes `sort_order`)
- Rename project (`r`)
- Remove project (`d` with confirmation)
- "Save as project?" prompt when entering Current dir with no saved project

## 3. Add-project modal — refinements

- Show how many sessions live under the highlighted directory (live count — query DB on cursor change)
- Show whether a dir already exists as a project (dimmed + tag)
- Discovered-cwds shortcut: a small header showing cwds we've seen sessions in, ranked by recency. One Enter to add.
- Toggle hidden dirs with a key (currently always hidden)
- Optional rename step on save

## 4. Sessions screen — more color

Tool-name chips landed (orange/teal/purple/pink). Still:

- Scope chip in the list title (`/home/kester/mainframe`) styled distinctly
- Archived indicator on rows (dim + tag) — important when `--archived` is on
- Missing indicator on rows
- Status line: success green, error red
- Selection highlight that respects the tool color of the selected row

## 5. End-to-end resume test

`syscall.Exec` paths are wired for all four tools but none have actually been triggered. Pick a throwaway session of each kind, verify the handoff feels clean (terminal state restored, alt-screen exited, parent process gone).

## 6. Live refresh follow-ups

5s tea.Tick polling for file-based tools shipped. Still open:

- **mtime-skip** in the per-tick parse so we only re-read files that changed since last sync
- **fsnotify** for true event-driven updates (WSL reliability is iffy — only if polling proves laggy)
- Decide whether to ever poll Amp on a long cadence (current decision: no, R-only)

## 7. Name the tool

`agent-sessions` is a placeholder. Candidates: `agenda`, `threadwise`, `sessions`, `recall`, `mux`. Open to anything.

---

## Deferred / nice-to-have

- **Content search (FTS5).** Title + cwd is enough for now; full-text over messages is a separate effort (lazy indexing, ranked snippets).
- **Codex title quality.** Some Codex sessions start with pasted terminal output, which becomes a noisy "title." Could filter out shell-prompt patterns.
- **Recently-opened sort boost.** Bump a session that was opened in the last hour.
- **Double-click to resume.** Currently mouse selects but doesn't resume.
- **Cross-tool actions.** Replay a Codex session in Claude. Out of scope.
- **Multi-machine / sync.** Punt.
- **Performance.** Re-parses every session file on each launch. With ~150 sessions it's ~1s; if it grows past ~1000 we'd want mtime-skip more urgently.
