package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type claudeRecord struct {
	Type      string          `json:"type"`
	AITitle   string          `json:"aiTitle,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	CWD       string          `json:"cwd,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func discoverClaude() ([]Session, []error) {
	var (
		sessions []Session
		errs     []error
	)
	root := claudeProjectsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	live := claudeLiveSessions()
	for _, projEnt := range entries {
		if !projEnt.IsDir() {
			continue
		}
		projDir := filepath.Join(root, projEnt.Name())

		files, err := os.ReadDir(projDir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, fEnt := range files {
			if fEnt.IsDir() || !strings.HasSuffix(fEnt.Name(), ".jsonl") {
				continue
			}
			fp := filepath.Join(projDir, fEnt.Name())
			sess, perr := parseClaudeFile(fp)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			if live[sess.SessionUUID] {
				sess.Live = true
			}
			sessions = append(sessions, sess)
		}
	}
	return sessions, errs
}

// claudeLiveSessions reads ~/.claude/sessions/*.json sidecars (one per
// claude process) and returns the set of sessionIds whose owning PID is
// still alive. Used to mark "open in another terminal" so the user doesn't
// try to --resume a locked session.
func claudeLiveSessions() map[string]bool {
	out := map[string]bool{}
	dir := filepath.Join(homeDir(), ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	type sidecar struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s sidecar
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.PID <= 0 || s.SessionID == "" {
			continue
		}
		if pidAlive(s.PID) {
			out[s.SessionID] = true
		}
	}
	return out
}

// pidAlive reports whether a process with the given PID exists. Uses the
// POSIX trick of signal 0 — checks for permission/existence without sending
// anything.
func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func parseClaudeFile(fp string) (Session, error) {
	info, err := os.Stat(fp)
	if err != nil {
		return Session{}, err
	}
	uuid := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	s := Session{
		Tool:        "claude",
		SessionUUID: uuid,
		FilePath:    fp,
		LastActive:  info.ModTime(),
		Size:        info.Size(),
	}

	f, err := os.Open(fp)
	if err != nil {
		return s, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var gotTitle, gotFirstMsg bool
	for sc.Scan() {
		if gotTitle && gotFirstMsg && !s.StartedAt.IsZero() && s.CWD != "" {
			break
		}
		var r claudeRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if s.StartedAt.IsZero() && r.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, r.Timestamp); err == nil {
				s.StartedAt = t
			}
		}
		// Claude embeds the real cwd in every record; the per-project slug
		// directory name is lossy and not authoritative.
		if s.CWD == "" && r.CWD != "" {
			s.CWD = r.CWD
		}
		switch r.Type {
		case "ai-title":
			if !gotTitle && r.AITitle != "" {
				s.Title = r.AITitle
				s.TitleSource = "ai"
				gotTitle = true
			}
		case "user":
			if !gotFirstMsg && len(r.Message) > 0 {
				var m claudeMessage
				if err := json.Unmarshal(r.Message, &m); err == nil && m.Role == "user" {
					if txt := extractTextFromContent(m.Content); txt != "" {
						s.FirstMsg = txt
						if !gotTitle {
							s.Title = firstNChars(cleanInline(txt), 80)
							s.TitleSource = "first-msg"
						}
						gotFirstMsg = true
					}
				}
			}
		}
	}
	return s, sc.Err()
}
