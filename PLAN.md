# Agent Session Browser

A CLI that finds, searches, previews, and resumes AI coding agent sessions across Codex, Claude Code, Amp, and Pi.

## UX target

Two-pane TUI, keyboard + mouse both first-class:
- **Left**: filtered session list — `tool · when · title · cwd`
- **Right**: preview of the last turn or two (depends on size)
- As-you-type token search across title, cwd, tool, and first message
- `Enter` (or double-click) → resume in the original tool, exec-replacing into its native CLI
- `a` → archive / unarchive selected session
- `R` → force re-scan of source dirs
- Mouse: click to select, scroll wheel in either pane, click filter to focus

## Where sessions live

| Tool   | Source                                                      | How we discover                                       |
|--------|-------------------------------------------------------------|-------------------------------------------------------|
| Codex  | `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl`    | Walk filesystem, parse JSONL. cwd in `session_meta`.  |
| Claude | `~/.claude/projects/<cwd-slug>/<uuid>.jsonl`                | Walk filesystem, parse JSONL. cwd from project slug (lossy for paths with literal dashes — see below). |
| Amp    | **Server-backed** at `https://ampcode.com`; partial local cache at `~/.local/share/amp/threads/` | Shell out to `amp threads list --include-archived --json`. cwd from `tree` field (file:// URI). |
| Pi     | `~/.pi/agent/sessions/<cwd-slug>/<ts>_<uuid>.jsonl`         | Walk filesystem, parse JSONL. cwd from `session` record. |

Notes:
- **Amp** has dozens of threads on the server but only ~2 cached locally. We can't list them by walking the filesystem; we must call the CLI. If `amp` isn't on PATH and `~/.amp/bin/amp` doesn't exist, we skip the tool.
- **Claude slug decoding** is lossy when a directory name contains a literal `-` (e.g. `OneDrive - HRSD`). For *display* this is cosmetic; for *cwd filtering*, we should slug-encode the comparison cwd at query time rather than trust the decoded path.
- We treat cwd as a per-session property and filter at runtime regardless of physical layout.

## Directory scoping

Default: show sessions whose cwd is the current working directory **or any descendant**.

Flags:
- `--all` — disregard cwd filter
- `--dir <path>` — additional scope root (repeatable)
- Config at `~/.config/agent-sessions/config.toml` for sticky extra roots

## Session titles

| Tool   | Native title? | Source                                                                 |
|--------|---------------|------------------------------------------------------------------------|
| Claude | **Yes**       | JSONL records with `"type":"ai-title"`, `aiTitle` field. ~75% coverage. |
| Codex  | No            | Has only model-reasoning summaries (not session titles). Derive from first user message. |
| Amp    | **Yes**       | Top-level `title` field on non-empty threads. cwd from `env.initial.trees[0].uri`. |
| Pi     | No            | Derive from first user message.                                        |

**v1 strategy**: use Claude's `aiTitle` where present; otherwise first ~60 chars of first user turn (cleaned).
**v2 (stretch)**: optional LLM-summarize the no-title tools, cached to `~/.cache/agent-sessions/titles.json` keyed by session ID. Lazy: only on first view.

## Resume commands

| Tool   | Command                            | Notes                                          |
|--------|------------------------------------|------------------------------------------------|
| Codex  | `codex resume <session-uuid>`      |                                                |
| Claude | `claude --resume <session-uuid>`   | Accepts partial UUID                           |
| Amp    | `amp threads continue <thread-id>` | thread-id includes the `T-` prefix             |
| Pi     | `pi --session <uuid-or-path>`      | Partial UUID supported                         |

Strategy: `exec` the native CLI so the user lands cleanly in the resumed session. No wrapping, no PTY.

## Tech stack

**Go + Bubble Tea.** Single static binary, fast startup, mouse-friendly, ecosystem is purpose-built for this kind of TUI. The current dependency set requires Go 1.25+.

Key deps:
- `charmbracelet/bubbletea` — the runtime
- `charmbracelet/bubbles` — list, viewport, textinput (mouse support built in)
- `charmbracelet/lipgloss` — styling
- `modernc.org/sqlite` — pure-Go SQLite driver (no CGO; keeps cross-compile / `go install` painless)

## Local cache database

The agent tools' files are source of truth — we never write to them. Our own SQLite DB at `~/.local/share/seshr/sessions.db` stores indexed metadata plus our own user state (archive flag, last-opened, etc.).

Update flow on launch:
1. Walk each tool's source dir, collect file paths + mtimes
2. For new files or files with changed mtime → re-parse, upsert
3. For files no longer on disk → mark `missing=1` (don't drop)
4. Render UI from DB (fast)

Force re-scan: `--refresh` flag or `R` in-app.

Schema sketch:
```sql
sessions(
  id TEXT PRIMARY KEY,             -- "<tool>:<uuid>"
  tool TEXT, session_uuid TEXT,
  file_path TEXT, file_mtime INTEGER, file_size INTEGER,
  cwd TEXT, started_at INTEGER, last_active INTEGER,
  title TEXT, title_source TEXT,   -- "ai" | "first-msg" | "manual"
  first_msg TEXT,
  archived INTEGER DEFAULT 0,
  missing INTEGER DEFAULT 0,
  opened_at INTEGER, open_count INTEGER DEFAULT 0
)
```

## User state & actions

State we own (not derivable from source files):
- `archived` — hidden from default view; toggle with `a`. Source file never touched.
- `opened_at` / `open_count` — for recency hints and "recently used" ordering.
- (later) starred, manual rename.

Views:
- **Active** (default) — `archived=0 AND missing=0`
- **Archived** — toggle to view, can unarchive
- **Missing** — files we used to see but disappeared (e.g. user wiped a tool's data dir)

## Build phases

1. **Bootstrap** — go.mod, deps, project layout
2. **Discovery** — per-tool parsers → unified `Session` record
3. **DB layer** — schema, upsert, query, mtime-based incremental sync
4. **CLI shell** — `agent-sessions list/refresh/archive` for headless verification
5. **TUI** — Bubble Tea two-pane with mouse, filter input, archive keybinding
6. **Resume** — exec the right tool's CLI

## Open questions

- Preview budget — show full last turn even if huge? Probably truncate at ~50 lines with a "…" indicator.
- Final tool name (currently the folder is `agent-sessions`).

## What's next

See **TODO.md** for the prioritized roadmap. The next major piece is project-based navigation (picker as first screen) — the flat list with `--all` / cwd-prefix filter is v1 only.

## Non-goals (v1)

- Editing sessions
- Cross-tool replay (e.g., open a Codex session in Claude)
- Multi-user / sync
- Auth or remote storage
