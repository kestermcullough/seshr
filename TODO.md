# TODO

Living roadmap. Top of the list is "do next."

---

## 1. Live-session detection for Codex, Pi, Amp

Claude version landed (reads `~/.claude/sessions/*.json` pid sidecars). Three siblings to consider:

- **Codex**: `~/.codex/sessions/...` is just JSONL — no sidecars. Could check open file handles via `/proc/<pid>/fd/*` or scan `ps -e` for `codex` processes and parse their argv for `--resume <uuid>`. Latter is cheaper and matches Claude's signal.
- **Pi**: same story, scan `ps` for `pi --session <uuid>`/`pi --resume`.
- **Amp**: locally nothing reliable; the API has the answer (active threads on server). Punt unless we want to ask the CLI.

When live for any tool: same UI badge, same fork-on-resume behavior where the tool supports it (Codex/Pi don't have a `--fork-session` equivalent — we'd just warn and resume anyway).

## 2. Project picker — polish

- Manual reorder (`J`/`K` to push pinned rows up/down → writes `sort_order`)
- Rename project (`r`)
- Remove project (`d` with confirmation)
- "Save as project?" prompt when entering Current dir with no saved project

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
