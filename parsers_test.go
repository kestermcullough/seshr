package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCodexFileFallsBackToFilenameID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-name.jsonl")
	data := `{"type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := parseCodexFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionUUID != "custom-name" {
		t.Fatalf("SessionUUID = %q", s.SessionUUID)
	}
}

func TestParsePiFileFallsBackToFilenameID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20260529_deadbeef.jsonl")
	data := `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := parsePiFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionUUID != "deadbeef" {
		t.Fatalf("SessionUUID = %q", s.SessionUUID)
	}
}
