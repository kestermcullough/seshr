package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ampThread struct {
	V         int          `json:"v"`
	ID        string       `json:"id"`
	Created   int64        `json:"created"` // unix ms
	Messages  []ampMessage `json:"messages"`
	AgentMode string       `json:"agentMode"`
	Title     string       `json:"title,omitempty"`
	Env       *ampEnv      `json:"env,omitempty"`
}

type ampEnv struct {
	Initial struct {
		Trees []struct {
			DisplayName string `json:"displayName"`
			URI         string `json:"uri"`
		} `json:"trees"`
	} `json:"initial"`
}

type ampMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func discoverAmp() ([]Session, []error) {
	var (
		sessions []Session
		errs     []error
	)
	root := ampThreadsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fp := filepath.Join(root, e.Name())
		s, perr := parseAmpFile(fp)
		if perr != nil {
			errs = append(errs, perr)
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, errs
}

func parseAmpFile(fp string) (Session, error) {
	info, err := os.Stat(fp)
	if err != nil {
		return Session{}, err
	}
	s := Session{
		Tool:       "amp",
		FilePath:   fp,
		LastActive: info.ModTime(),
		Size:       info.Size(),
	}

	f, err := os.Open(fp)
	if err != nil {
		return s, err
	}
	defer f.Close()
	var t ampThread
	if err := json.NewDecoder(f).Decode(&t); err != nil {
		return s, err
	}
	s.SessionUUID = strings.TrimPrefix(t.ID, "T-")
	if t.Created > 0 {
		s.StartedAt = time.UnixMilli(t.Created)
	}
	if t.Title != "" {
		s.Title = t.Title
		s.TitleSource = "ai"
	}
	if t.Env != nil && len(t.Env.Initial.Trees) > 0 {
		s.CWD = strings.TrimPrefix(t.Env.Initial.Trees[0].URI, "file://")
	}
	for _, m := range t.Messages {
		if m.Role != "user" {
			continue
		}
		if txt := extractTextFromContent(m.Content); txt != "" {
			s.FirstMsg = txt
			if s.Title == "" {
				s.Title = firstNChars(cleanInline(txt), 80)
				s.TitleSource = "first-msg"
			}
			break
		}
	}
	return s, nil
}
