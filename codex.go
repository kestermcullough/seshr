package main

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type codexRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
	Model     string `json:"model"`
}

type codexEventPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var codexFilenameRe = regexp.MustCompile(`^rollout-(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2})-([0-9a-f-]+)\.jsonl$`)

func discoverCodex() ([]Session, []error) {
	var (
		sessions []Session
		errs     []error
	)
	root := codexSessionsDir()
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
		s, perr := parseCodexFile(p)
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

func parseCodexFile(fp string) (Session, error) {
	info, err := os.Stat(fp)
	if err != nil {
		return Session{}, err
	}
	s := Session{
		Tool:       "codex",
		FilePath:   fp,
		LastActive: info.ModTime(),
		Size:       info.Size(),
	}
	if m := codexFilenameRe.FindStringSubmatch(filepath.Base(fp)); m != nil {
		if t, err := time.Parse("2006-01-02T15-04-05", m[1]); err == nil {
			s.StartedAt = t
		}
		s.SessionUUID = m[2]
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
		if gotFirstMsg && s.CWD != "" {
			break
		}
		var r codexRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		switch r.Type {
		case "session_meta":
			var m codexSessionMeta
			if err := json.Unmarshal(r.Payload, &m); err == nil {
				if s.SessionUUID == "" {
					s.SessionUUID = m.ID
				}
				if s.CWD == "" {
					s.CWD = m.CWD
				}
			}
		case "event_msg":
			if !gotFirstMsg {
				var p codexEventPayload
				if err := json.Unmarshal(r.Payload, &p); err == nil &&
					p.Type == "user_message" &&
					strings.TrimSpace(p.Message) != "" {
					s.FirstMsg = p.Message
					s.Title = firstNChars(cleanInline(p.Message), 80)
					s.TitleSource = "first-msg"
					gotFirstMsg = true
				}
			}
		}
	}
	if s.SessionUUID == "" {
		s.SessionUUID = fallbackSessionIDFromPath(fp)
	}
	return s, sc.Err()
}
