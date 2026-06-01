package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderPreviewCachedUsesSessionMTimeKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeTestFile(t, path, codexUserMessageJSONL("needle"))
	s := Session{
		Tool:        "codex",
		SessionUUID: "cache-test",
		FilePath:    path,
		LastActive:  time.Unix(1, 0),
	}
	cache := transcriptMatchCache{}

	first := renderPreviewCached(s, []string{"needle"}, cache)
	if !strings.Contains(first, "search match") || !strings.Contains(first, "needle") {
		t.Fatalf("expected initial cached preview to show match, got:\n%s", first)
	}

	writeTestFile(t, path, codexUserMessageJSONL("other"))
	sameMTime := renderPreviewCached(s, []string{"needle"}, cache)
	if !strings.Contains(sameMTime, "search match") || !strings.Contains(sameMTime, "needle") {
		t.Fatalf("expected same mtime to reuse cached match, got:\n%s", sameMTime)
	}

	s.LastActive = time.Unix(2, 0)
	newMTime := renderPreviewCached(s, []string{"needle"}, cache)
	if strings.Contains(newMTime, "search match") {
		t.Fatalf("expected changed mtime to miss old cached match, got:\n%s", newMTime)
	}
}

func codexUserMessageJSONL(message string) string {
	return `{"type":"event_msg","payload":{"type":"user_message","message":"` + message + `"}}` + "\n"
}
