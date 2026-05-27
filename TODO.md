# TODO

Living roadmap. Top of the list is "do next."

---

## 1. Transcript-search follow-ups (basic version shipped)

The preview now surfaces the most recent transcript turn containing every search token, with highlights. Follow-ups worth picking up later:
- **Cache** match results per (session_id, query) so we don't re-scan files on every keystroke. With ~150 sessions and ~3 MB max per file, perceived perf is fine for now, but this would help if the corpus grows.
- **Match navigation**: keys like `n` / `N` to step through earlier matches instead of jumping straight to the most recent. Sessions with many hits would benefit.
- **More context**: show a turn or two before/after the match for orientation, not just the match in isolation.

## 2. Mouse — click-to-select (shelved; wheel scroll works)

Tried a first pass with hard-coded Y offsets (`sessionsFirstItemY=6`, `pickerFirstItemY=3`) derived from bubbles/list's default rendering math. In practice clicks landed wrong — terminal layout differs from the assumed model somehow.

What needs to change before another attempt:
- **Calibrate at runtime**, not via constants. One option: render the View() text, scan line-by-line for known item Title prefixes (e.g. the `[CLAUDE]` chip), build an absolute-Y → item-index map per render. Cost: O(lines) per click, cheap.
- Another option: instrument with a temporary debug overlay (status line shows click Y + current cursor Y) and use real measurements to figure out what the offset actually is.
- Code for the previous attempt is still in `tui.go` (`handleSessionListClick`, `handlePickerClick`, `clickToVisibleIndex`, `isDoubleClickNear`, `pickerSkipSpacer`, plus `lastClickTime`/`lastClickY` fields) but not wired in — easy to re-enable once a robust coord-to-index function exists.

Once click-to-select works, double-click-to-resume is ~10 lines (the previous attempt's debounce logic).

## 3. Picker spacer is selectable

The blank row that separates saved projects from the action rows is currently a selectable list item that Enter no-ops on. Should be skipped during j/k navigation entirely. Needs a custom list delegate or a wrapper that auto-advances when the cursor lands on a spacer.

## 4. Live-session detection for Codex, Pi, Amp

Claude version landed (reads `~/.claude/sessions/*.json` pid sidecars). Three siblings to consider:

- **Codex**: `~/.codex/sessions/...` is just JSONL — no sidecars. Could check open file handles via `/proc/<pid>/fd/*` or scan `ps -e` for `codex` processes and parse their argv for `--resume <uuid>`. Latter is cheaper and matches Claude's signal.
- **Pi**: same story, scan `ps` for `pi --session <uuid>`/`pi --resume`.
- **Amp**: locally nothing reliable; the API has the answer (active threads on server). Punt unless we want to ask the CLI.

When live for any tool: same UI badge, same fork-on-resume behavior where the tool supports it (Codex/Pi don't have a `--fork-session` equivalent — we'd just warn and resume anyway).

## 5. Settings — follow-ups (in-TUI screen shipped)

`S` from the picker opens a modal that shows per-tool retention and lets the user edit Claude's `cleanupPeriodDays` inline. CLI subcommand still works. Remaining ideas:
- One-keystroke "raise it now" prompt from the cleanup warning chip itself (currently `S` is needed)
- Quick presets in the settings modal (1=90, 2=180, 3=365, 4=99999) so users don't have to type
- (Stretch) write the same value to detected project-level `.claude/settings.json` files

## 6. Project picker — remaining polish

Rename / remove / reorder landed. Still:
- "Save as project?" prompt when entering Current dir without a matching saved project

## 7. Sessions screen — remaining color polish

Tool chips, archived/missing/live badges, and status colors landed. Still:
- Scope chip in the list title styled distinctly
- Selection highlight that respects the tool color of the selected row

## 8. Quit shortcut in sessions view

`q` types into the search bar now (intentional — as-you-type search). Currently `Esc` returns to picker; from picker, `q` quits. Consider:
- `Esc Esc` to quit directly from sessions view
- Or just live with the two-step flow

## 9. End-to-end resume test

`syscall.Exec` paths are wired for all four tools. Claude verified (this conversation's resume + fork-session path). Still need a hands-on:
- Codex (`codex resume <uuid>`)
- Amp (`amp threads continue T-<uuid>`)
- Pi (`pi --session <uuid>`)
- Edge case: alt-screen cleanup on resume failure (rename `claude` binary out of PATH temporarily and verify the terminal isn't left in a weird state)

## 10. Live refresh follow-ups

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
