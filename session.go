package main

import "time"

type Session struct {
	Tool        string    // "claude" | "codex" | "amp" | "pi"
	SessionUUID string    // tool's own session ID
	FilePath    string    // absolute path to the session file
	CWD         string    // best-effort working dir; may be empty
	StartedAt   time.Time // session start; best-effort
	LastActive  time.Time // file mtime (proxy for last activity)
	Title       string    // ai title if available, else trimmed first user message
	TitleSource string    // "ai" | "first-msg"
	FirstMsg    string    // full first user message text
	Size        int64     // file size in bytes
	Archived    bool      // user marked it hidden from default view
	Missing     bool      // source file/thread no longer present on disk
	Live        bool      // a process holding this session is currently running
}

func (s Session) ID() string { return s.Tool + ":" + s.SessionUUID }
