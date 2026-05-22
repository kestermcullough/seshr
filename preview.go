package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// renderPreview produces the right-pane content for a session. For local files
// it reads the last user/assistant turns. For Amp threads with no local file,
// it consults the per-thread cache (populated lazily by ampFetchCmd); cache
// miss falls back to a header + first-message placeholder while the fetch
// runs.
func renderPreview(s Session) string {
	header := previewHeader(s)

	if s.Tool == "amp" && s.FilePath == "" {
		if t, ok := ampThreadCached(s); ok {
			u, a := extractAmpTurnsFrom(t)
			return header + renderTurnsBody(u, a, s.FirstMsg)
		}
		body := "(loading content from amp…)\n\n"
		if s.FirstMsg != "" {
			body += "→ first user message\n" + truncate(s.FirstMsg, 1500) + "\n"
		}
		return header + body
	}

	if s.FilePath == "" {
		body := "(no local file available)\n\n"
		if s.FirstMsg != "" {
			body += "→ first user message\n" + truncate(s.FirstMsg, 1500) + "\n"
		}
		return header + body
	}

	lastUser, lastAssistant, err := extractLastTurns(s)
	if err != nil {
		return header + "(preview error: " + err.Error() + ")\n"
	}
	return header + renderTurnsBody(lastUser, lastAssistant, s.FirstMsg)
}

func previewHeader(s Session) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s\n", strings.ToUpper(s.Tool), s.Title))
	if s.CWD != "" {
		sb.WriteString("cwd: " + s.CWD + "\n")
	}
	if !s.LastActive.IsZero() {
		sb.WriteString("last active: " + s.LastActive.Format("2006-01-02 15:04") + "\n")
	}
	sb.WriteString("id: " + s.SessionUUID + "\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n\n")
	return sb.String()
}

func renderTurnsBody(lastUser, lastAssistant, firstMsg string) string {
	if lastUser == "" && lastAssistant == "" {
		if firstMsg != "" {
			return "→ first user message\n" + truncate(firstMsg, 1500) + "\n"
		}
		return "(no turns extracted)\n"
	}
	var sb strings.Builder
	if lastUser != "" {
		sb.WriteString("→ user\n" + truncate(lastUser, 1500) + "\n\n")
	}
	if lastAssistant != "" {
		sb.WriteString("← assistant\n" + truncate(lastAssistant, 2000) + "\n")
	}
	return sb.String()
}

func extractLastTurns(s Session) (string, string, error) {
	switch s.Tool {
	case "claude":
		return extractClaudeTurns(s.FilePath)
	case "codex":
		return extractCodexTurns(s.FilePath)
	case "amp":
		return extractAmpTurns(s.FilePath)
	case "pi":
		return extractPiTurns(s.FilePath)
	}
	return "", "", fmt.Errorf("unsupported tool %q", s.Tool)
}

func extractClaudeTurns(fp string) (string, string, error) {
	f, err := os.Open(fp)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var lastUser, lastAssistant string
	for sc.Scan() {
		var r claudeRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Type != "user" && r.Type != "assistant" {
			continue
		}
		var m claudeMessage
		if err := json.Unmarshal(r.Message, &m); err != nil {
			continue
		}
		txt := extractTextFromContent(m.Content)
		if txt == "" {
			continue
		}
		switch r.Type {
		case "user":
			lastUser = txt
		case "assistant":
			lastAssistant = txt
		}
	}
	return lastUser, lastAssistant, sc.Err()
}

func extractCodexTurns(fp string) (string, string, error) {
	f, err := os.Open(fp)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var lastUser, lastAssistant string
	for sc.Scan() {
		var r codexRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Type != "event_msg" {
			continue
		}
		var p codexEventPayload
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			continue
		}
		switch p.Type {
		case "user_message":
			if strings.TrimSpace(p.Message) != "" {
				lastUser = p.Message
			}
		case "agent_message":
			if strings.TrimSpace(p.Message) != "" {
				lastAssistant = p.Message
			}
		}
	}
	return lastUser, lastAssistant, sc.Err()
}

func extractAmpTurns(fp string) (string, string, error) {
	f, err := os.Open(fp)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	var t ampThread
	if err := json.NewDecoder(f).Decode(&t); err != nil {
		return "", "", err
	}
	u, a := extractAmpTurnsFrom(&t)
	return u, a, nil
}

func extractAmpTurnsFrom(t *ampThread) (lastUser, lastAssistant string) {
	for _, m := range t.Messages {
		txt := extractTextFromContent(m.Content)
		if txt == "" {
			continue
		}
		switch m.Role {
		case "user":
			lastUser = txt
		case "assistant":
			lastAssistant = txt
		}
	}
	return
}

func extractPiTurns(fp string) (string, string, error) {
	f, err := os.Open(fp)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var lastUser, lastAssistant string
	for sc.Scan() {
		var r piRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Type != "message" || len(r.Message) == 0 {
			continue
		}
		var m piMessage
		if err := json.Unmarshal(r.Message, &m); err != nil {
			continue
		}
		txt := extractTextFromContent(m.Content)
		if txt == "" {
			continue
		}
		switch m.Role {
		case "user":
			lastUser = txt
		case "assistant":
			lastAssistant = txt
		}
	}
	return lastUser, lastAssistant, sc.Err()
}

func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "\n…(truncated)"
}
