package main

import (
	"encoding/json"
	"testing"
)

func TestExtractTextFromContentConcatenatesTextBlocks(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"first"},
		{"type":"tool_use","text":"ignored"},
		{"type":"text","text":"second"}
	]`)
	if got := extractTextFromContent(raw); got != "first\nsecond" {
		t.Fatalf("got %q", got)
	}
}

func TestFallbackSessionIDFromPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/rollout-2026-05-29T10-00-00-abc.jsonl": "rollout-2026-05-29T10-00-00-abc",
		"/tmp/20260529_deadbeef.jsonl":               "deadbeef",
	}
	for path, want := range tests {
		if got := fallbackSessionIDFromPath(path); got != want {
			t.Fatalf("fallbackSessionIDFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}
