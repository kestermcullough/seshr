package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderPreview produces the right-pane content for a session. With an active
// search query (tokens non-empty), it surfaces the most recent transcript
// turn containing every token instead of the default last user/assistant
// turn — so the preview shows you *why* this row matched. Falls back to the
// default behavior when there's no query or no match in the transcript.
func renderPreview(s Session, tokens []string) string {
	header := previewHeader(s)

	if len(tokens) > 0 {
		if m := findTranscriptMatch(s, tokens); m.found {
			return header + renderTranscriptMatch(m, tokens)
		}
	}

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

// transcriptMatch is the result of scanning a session's transcript for the
// search query: the most recent turn containing every token.
type transcriptMatch struct {
	found bool
	role  string // "user" | "assistant"
	text  string
	index int // 1-based turn index
	total int // total counted turns
}

func renderTranscriptMatch(m transcriptMatch, tokens []string) string {
	label := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"}).
		Bold(true).
		Render(fmt.Sprintf("⟡ search match · turn %d of %d", m.index, m.total))
	role := "→ user"
	if m.role == "assistant" {
		role = "← assistant"
	}
	return label + "\n" + role + "\n" +
		highlightTokens(truncate(m.text, 2500), tokens) + "\n"
}

func findTranscriptMatch(s Session, tokens []string) transcriptMatch {
	if len(tokens) == 0 {
		return transcriptMatch{}
	}
	switch s.Tool {
	case "claude":
		return findClaudeMatch(s, tokens)
	case "codex":
		return findCodexMatch(s, tokens)
	case "amp":
		return findAmpMatch(s, tokens)
	case "pi":
		return findPiMatch(s, tokens)
	}
	return transcriptMatch{}
}

func containsAllTokens(text string, tokens []string) bool {
	lo := strings.ToLower(text)
	for _, t := range tokens {
		if !strings.Contains(lo, t) {
			return false
		}
	}
	return true
}

func findClaudeMatch(s Session, tokens []string) transcriptMatch {
	if s.FilePath == "" {
		return transcriptMatch{}
	}
	f, err := os.Open(s.FilePath)
	if err != nil {
		return transcriptMatch{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var match transcriptMatch
	turn := 0
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
		turn++
		if containsAllTokens(txt, tokens) {
			match = transcriptMatch{found: true, role: r.Type, text: txt, index: turn}
		}
	}
	match.total = turn
	return match
}

func findCodexMatch(s Session, tokens []string) transcriptMatch {
	if s.FilePath == "" {
		return transcriptMatch{}
	}
	f, err := os.Open(s.FilePath)
	if err != nil {
		return transcriptMatch{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var match transcriptMatch
	turn := 0
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
		if p.Type != "user_message" && p.Type != "agent_message" {
			continue
		}
		if strings.TrimSpace(p.Message) == "" {
			continue
		}
		role := "user"
		if p.Type == "agent_message" {
			role = "assistant"
		}
		turn++
		if containsAllTokens(p.Message, tokens) {
			match = transcriptMatch{found: true, role: role, text: p.Message, index: turn}
		}
	}
	match.total = turn
	return match
}

func findAmpMatch(s Session, tokens []string) transcriptMatch {
	var t *ampThread
	if s.FilePath != "" {
		f, err := os.Open(s.FilePath)
		if err == nil {
			var tt ampThread
			if json.NewDecoder(f).Decode(&tt) == nil {
				t = &tt
			}
			f.Close()
		}
	}
	if t == nil {
		if cached, ok := ampThreadCached(s); ok {
			t = cached
		}
	}
	if t == nil {
		return transcriptMatch{}
	}
	var match transcriptMatch
	turn := 0
	for _, m := range t.Messages {
		txt := extractTextFromContent(m.Content)
		if txt == "" {
			continue
		}
		turn++
		if containsAllTokens(txt, tokens) {
			match = transcriptMatch{found: true, role: m.Role, text: txt, index: turn}
		}
	}
	match.total = turn
	return match
}

func findPiMatch(s Session, tokens []string) transcriptMatch {
	if s.FilePath == "" {
		return transcriptMatch{}
	}
	f, err := os.Open(s.FilePath)
	if err != nil {
		return transcriptMatch{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var match transcriptMatch
	turn := 0
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
		turn++
		if containsAllTokens(txt, tokens) {
			match = transcriptMatch{found: true, role: m.Role, text: txt, index: turn}
		}
	}
	match.total = turn
	return match
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
