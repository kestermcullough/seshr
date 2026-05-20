package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type claudeRecord struct {
	Type      string          `json:"type"`
	AITitle   string          `json:"aiTitle,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// decodeClaudeSlug reverses Claude's project-directory naming back to a path,
// best effort. The encoding maps "/" -> "-", so paths containing literal
// dashes round-trip lossily. We use this for display only; matching by cwd
// should slug-encode the candidate path instead.
func decodeClaudeSlug(slug string) string {
	if !strings.HasPrefix(slug, "-") {
		return slug
	}
	return strings.ReplaceAll(slug, "-", "/")
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
	for _, projEnt := range entries {
		if !projEnt.IsDir() {
			continue
		}
		projDir := filepath.Join(root, projEnt.Name())
		cwd := decodeClaudeSlug(projEnt.Name())

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
			sess, perr := parseClaudeFile(fp, cwd)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			sessions = append(sessions, sess)
		}
	}
	return sessions, errs
}

func parseClaudeFile(fp, cwd string) (Session, error) {
	info, err := os.Stat(fp)
	if err != nil {
		return Session{}, err
	}
	uuid := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	s := Session{
		Tool:        "claude",
		SessionUUID: uuid,
		FilePath:    fp,
		CWD:         cwd,
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
		if gotTitle && gotFirstMsg && !s.StartedAt.IsZero() {
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
