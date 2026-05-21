package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Amp threads live primarily on Sourcegraph's server (https://ampcode.com); the
// few JSON files under ~/.local/share/amp/threads/ are a partial local cache.
// The canonical source is `amp threads list --json`, which returns AI-titled
// metadata for every thread the user has on every machine.

// ampThread is the schema of a locally cached thread file. We don't use it for
// listing (the CLI is canonical) but we do read these files when previewing.
type ampThread struct {
	V         int          `json:"v"`
	ID        string       `json:"id"`
	Created   int64        `json:"created"`
	Messages  []ampMessage `json:"messages"`
	AgentMode string       `json:"agentMode"`
	Title     string       `json:"title,omitempty"`
}

type ampMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ampListEntry struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Updated      string `json:"updated"`
	Tree         string `json:"tree"` // file:// URI of the workspace root
	MessageCount int    `json:"messageCount"`
}

func discoverAmp() ([]Session, []error) {
	bin := findAmpBinary()
	if bin == "" {
		return nil, nil
	}

	cmd := exec.Command(bin, "threads", "list", "--include-archived", "--json")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, []error{fmt.Errorf("amp threads list failed: %s", strings.TrimSpace(string(ee.Stderr)))}
		}
		return nil, []error{fmt.Errorf("amp threads list: %w", err)}
	}

	var entries []ampListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, []error{fmt.Errorf("parse amp threads list output: %w", err)}
	}

	sessions := make([]Session, 0, len(entries))
	localDir := ampThreadsDir()
	for _, e := range entries {
		s := Session{
			Tool:        "amp",
			SessionUUID: strings.TrimPrefix(e.ID, "T-"),
			Title:       e.Title,
			TitleSource: "ai",
		}
		if t, err := time.Parse(time.RFC3339, e.Updated); err == nil {
			s.LastActive = t
			s.StartedAt = t // "updated" is the best signal the API gives us
		}
		s.CWD = decodeFileURI(e.Tree)

		// If we happen to have a local cache file, point at it and fill in size.
		cachePath := filepath.Join(localDir, e.ID+".json")
		if info, err := os.Stat(cachePath); err == nil {
			s.FilePath = cachePath
			s.Size = info.Size()
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func decodeFileURI(uri string) string {
	if uri == "" {
		return ""
	}
	p := strings.TrimPrefix(uri, "file://")
	if dec, err := url.PathUnescape(p); err == nil {
		return dec
	}
	return p
}

func findAmpBinary() string {
	if p, err := exec.LookPath("amp"); err == nil {
		return p
	}
	candidate := filepath.Join(homeDir(), ".amp", "bin", "amp")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
