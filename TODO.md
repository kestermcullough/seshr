# TODO

Living roadmap. Top of the list is "do next."

---

## 1. Transcript-aware search hits

When the user is searching, the preview pane currently just shows the *last* user/assistant turn — which usually doesn't contain the search query. Instead, when there's an active query, scan the session's transcript for the most recent line containing the match and render that section (with matched substrings highlighted, same style as the list rows). For sessions with no transcript match (Amp without local cache, etc.) fall back to current behavior.

This is the natural extension of the title/cwd highlighting that just landed.

## 2. Mouse — click-to-select and click-to-resume

Mouse-wheel scrolling is now routed (left pane scrolls list, right scrolls preview). Still missing:
- **Click a row** to select it (bubbles/list doesn't do this by default — needs custom delegate or a translate-click-to-cursor helper)
- **Double-click** a row to resume (would need debounce tracking; ~300ms window)
- Click on the search input to focus / clear (already focused, but explicit click is more discoverable)

## 3. Picker spacer is selectable

The blank row that separates saved projects from the action rows is currently a selectable list item that Enter no-ops on. Should be skipped during j/k navigation entirely. Needs a custom list delegate or a wrapper that auto-advances when the cursor lands on a spacer.

## 4. Live-session detection for Codex, Pi, Amp

Claude version landed (reads `~/.claude/sessions/*.json` pid sidecars). Three siblings to consider:

- **Codex**: `~/.codex/sessions/...` is just JSONL — no sidecars. Could check open file handles via `/proc/<pid>/fd/*` or scan `ps -e` for `codex` processes and parse their argv for `--resume <uuid>`. Latter is cheaper and matches Claude's signal.
- **Pi**: same story, scan `ps` for `pi --session <uuid>`/`pi --resume`.
- **Amp**: locally nothing reliable; the API has the answer (active threads on server). Punt unless we want to ask the CLI.

When live for any tool: same UI badge, same fork-on-resume behavior where the tool supports it (Codex/Pi don't have a `--fork-session` equivalent — we'd just warn and resume anyway).

## 5. Project picker — remaining polish

Rename / remove / reorder landed. Still:
- "Save as project?" prompt when entering Current dir without a matching saved project

## 3. Sessions screen — remaining color polish

Tool chips, archived/missing/live badges, and status colors landed. Still:
- Scope chip in the list title styled distinctly
- Selection highlight that respects the tool color of the selected row

## 4. Quit shortcut in sessions view

`q` types into the search bar now (intentional — as-you-type search). Currently `Esc` returns to picker; from picker, `q` quits. Consider:
- `Esc Esc` to quit directly from sessions view
- Or just live with the two-step flow

## 5. End-to-end resume test

`syscall.Exec` paths are wired for all four tools. Claude verified (this conversation's resume + fork-session path). Still need a hands-on:
- Codex (`codex resume <uuid>`)
- Amp (`amp threads continue T-<uuid>`)
- Pi (`pi --session <uuid>`)
- Edge case: alt-screen cleanup on resume failure (rename `claude` binary out of PATH temporarily and verify the terminal isn't left in a weird state)

## 6. Live refresh follow-ups

5s tea.Tick polling for file-based tools shipped. Still open:
- **mtime-skip** in the per-tick parse so we only re-read files that changed since last sync
- **fsnotify** for true event-driven updates (WSL reliability is iffy — only if polling proves laggy)
- Decide whether to ever poll Amp on a long cadence (current decision: no, R-only)

---

## Deferred / nice-to-have

- **Content search (FTS5).** Title + cwd is enough for now; full-text over messages is a separate effort (lazy indexing, ranked snippets).
- **Codex title quality.** Some Codex sessions start with pasted terminal output, which becomes a noisy "title." Could filter out shell-prompt patterns.
- **Recently-opened sort boost.** Bump a session that was opened in the last hour.
- **Double-click to resume.** Currently mouse selects but doesn't resume.
- **Add-project modal extras.** Toggle hidden dirs key; optional rename step on save.
- **Cross-tool actions.** Replay a Codex session in Claude. Out of scope.
- **Multi-machine / sync.** Punt.
- **Performance.** Re-parses every session file on each launch + tick. With ~150 sessions it's ~1s; if it grows past ~1000 we'd want mtime-skip more urgently.
