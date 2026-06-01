# seshr

A small terminal app that finds, searches, previews, and resumes your AI coding-agent sessions across **Claude Code**, **OpenAI Codex**, **Sourcegraph Amp**, and **Pi** — all in one searchable list.

It walks wherever each agent stores its session files, unifies them, lets you search by title / cwd / message content, previews the most-recent matching turn, and `exec`s straight back into the original tool's CLI to resume.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/kestermcullough/seshr/main/install.sh | sh
```

Requires Go 1.25+ (the script compiles from source via `go install`).

Or, manually:

```bash
go install github.com/kestermcullough/seshr@latest
```

The binary lands at `$(go env GOPATH)/bin/seshr`. Make sure that's on your PATH.

## Use

```bash
seshr                       # open the TUI: project picker → sessions list
seshr --list                # dump sessions to stdout, scoped to current dir
seshr --list --all          # …or every cwd
seshr --list --archived     # …including archived

seshr settings                                    # session-retention report per tool
seshr settings claude-cleanup-days 365            # stop Claude auto-deleting old sessions
```

### Keys in the TUI

**Picker** (first screen):
- `↑/↓` select, `enter` open, `+` add project
- `r` rename, `d` remove, `J/K` reorder a saved project
- `i` disk-usage breakdown, `/` filter, `q` quit

**Sessions** (after picking a project):
- type to filter (always-on search; matches title + cwd + tool + first message)
- `↑/↓` move, `enter` resume, `esc` back to picker
- `ctrl+r` rescan, `ctrl+a` archive selected, `ctrl+t` toggle showing archived

**Add-project modal**:
- type to filter the dir listing; arrow keys to navigate; `enter` on a folder to descend
- top of list surfaces "where you've already been" as one-Enter quick adds
- `enter` on `[Save THIS directory: …]` saves the current location as a project

## What's supported

| Tool | Discovery | Resume command | Notes |
|------|-----------|----------------|-------|
| Claude Code | `~/.claude/projects/.../*.jsonl` | `claude --resume <uuid>` (or `--fork-session` if live) | reads cwd from per-record fields; detects live sessions via pid sidecars |
| OpenAI Codex | `~/.codex/sessions/YYYY/MM/DD/*.jsonl` | `codex resume <uuid>` | |
| Sourcegraph Amp | `amp threads list --json` (server-backed); local cache populated on first preview via `amp threads export` | `amp threads continue T-<uuid>` | reuses whatever auth you've already given the `amp` CLI |
| Pi | `~/.pi/agent/sessions/.../*.jsonl` | `pi --session <uuid>` | |

## Storage

- **Metadata DB**: `~/.local/share/seshr/sessions.db` (SQLite)
- **Amp content cache**: `~/.local/share/seshr/amp-cache/T-<uuid>.json` (populated lazily, invalidated when the server's `updated` timestamp moves)

We never duplicate Claude / Codex / Pi session files — they're read in place. Press `i` in the picker for a full breakdown of disk usage.

## Status

Early but in active personal use. PLAN.md and TODO.md track what's built and what's next. No telemetry, no auth seshr asks for (each tool's CLI handles its own).

## License

Not yet specified — add a LICENSE file if you want to reuse code from here.
