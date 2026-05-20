package main

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type piRecord struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	CWD       string          `json:"cwd,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
}

type piMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func discoverPi() ([]Session, []error) {
	var (
		sessions []Session
		errs     []error
	)
	root := piSessionsDir()
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		s, perr := parsePiFile(p)
		if perr != nil {
			errs = append(errs, perr)
			return nil
		}
		sessions = append(sessions, s)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return sessions, errs
}

func parsePiFile(fp string) (Session, error) {
	info, err := os.Stat(fp)
	if err != nil {
		return Session{}, err
	}
	s := Session{
		Tool:       "pi",
		FilePath:   fp,
		LastActive: info.ModTime(),
		Size:       info.Size(),
	}

	f, err := os.Open(fp)
	if err != nil {
		return s, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var gotFirstMsg bool
	for sc.Scan() {
		if gotFirstMsg && s.CWD != "" && !s.StartedAt.IsZero() {
			break
		}
		var r piRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		switch r.Type {
		case "session":
			if s.SessionUUID == "" {
				s.SessionUUID = r.ID
			}
			if s.CWD == "" {
				s.CWD = r.CWD
			}
			if s.StartedAt.IsZero() && r.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339, r.Timestamp); err == nil {
					s.StartedAt = t
				}
			}
		case "message":
			if !gotFirstMsg && len(r.Message) > 0 {
				var m piMessage
				if err := json.Unmarshal(r.Message, &m); err == nil && m.Role == "user" {
					if txt := extractTextFromContent(m.Content); txt != "" {
						s.FirstMsg = txt
						s.Title = firstNChars(cleanInline(txt), 80)
						s.TitleSource = "first-msg"
						gotFirstMsg = true
					}
				}
			}
		}
	}
	return s, sc.Err()
}
