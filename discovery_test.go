package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiscoverAllDetailedWithFixtures(t *testing.T) {
	home := withIsolatedRuntime(t)

	writeTestFile(t,
		filepath.Join(home, ".claude", "projects", "repo", "claude-1.jsonl"),
		`{"type":"user","timestamp":"2026-05-29T10:00:00Z","cwd":"/work/claude","message":{"role":"user","content":[{"type":"text","text":"claude first"}]}}`+"\n"+
			`{"type":"ai-title","aiTitle":"Claude Title"}`+"\n",
	)
	writeTestFile(t,
		filepath.Join(home, ".claude", "sessions", "live.json"),
		`{"pid":4242,"sessionId":"claude-1"}`,
	)
	processIDIsLive = func(pid int) bool { return pid == 4242 }

	writeTestFile(t,
		filepath.Join(home, ".codex", "sessions", "2026", "05", "29", "rollout-2026-05-29T11-00-00-codex-1.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-1","cwd":"/work/codex","timestamp":"2026-05-29T11:00:00Z"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"codex first"}}`+"\n",
	)

	writeTestFile(t,
		filepath.Join(home, ".pi", "agent", "sessions", "repo", "20260529_pi-1.jsonl"),
		`{"type":"session","id":"pi-1","cwd":"/work/pi","timestamp":"2026-05-29T12:00:00Z"}`+"\n"+
			`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"pi first"}]}}`+"\n",
	)

	writeTestFile(t,
		filepath.Join(home, ".local", "share", "amp", "threads", "T-amp-1.json"),
		`{"id":"T-amp-1","messages":[{"role":"user","content":[{"type":"text","text":"amp first"}]}]}`,
	)
	ampList, err := json.Marshal([]ampListEntry{{
		ID:      "T-amp-1",
		Title:   "Amp Title",
		Updated: "2026-05-29T13:00:00Z",
		Tree:    "file:///work/amp%20repo",
	}})
	if err != nil {
		t.Fatal(err)
	}
	lookPath = func(name string) (string, error) {
		if name == "amp" {
			return "/fake/amp", nil
		}
		return "", exec.ErrNotFound
	}
	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		if name != "/fake/amp" || strings.Join(args, " ") != "threads list --include-archived --json" {
			return nil, fmt.Errorf("unexpected command: %s %v", name, args)
		}
		return ampList, nil
	}

	got := DiscoverAllDetailed()
	if len(got.Errors) != 0 {
		t.Fatalf("discovery errors: %v", got.Errors)
	}
	if strings.Join(got.CompleteTools, ",") != "amp,claude,codex,pi" {
		t.Fatalf("complete tools = %#v", got.CompleteTools)
	}
	if len(got.Sessions) != 4 {
		t.Fatalf("sessions = %d, want 4: %#v", len(got.Sessions), got.Sessions)
	}

	byID := map[string]Session{}
	for _, s := range got.Sessions {
		byID[s.ID()] = s
	}
	assertDiscoveredSession(t, byID["claude:claude-1"], "claude", "Claude Title", "/work/claude", true)
	assertDiscoveredSession(t, byID["codex:codex-1"], "codex", "codex first", "/work/codex", false)
	assertDiscoveredSession(t, byID["pi:pi-1"], "pi", "pi first", "/work/pi", false)
	assertDiscoveredSession(t, byID["amp:amp-1"], "amp", "Amp Title", "/work/amp repo", false)
}

func TestDiscoverAllDetailedSkipsUnavailableAmpScope(t *testing.T) {
	withIsolatedRuntime(t)

	got := DiscoverAllDetailed()
	if len(got.Errors) != 0 {
		t.Fatalf("discovery errors: %v", got.Errors)
	}
	if containsString(got.CompleteTools, "amp") {
		t.Fatalf("unavailable amp should not be marked complete: %#v", got.CompleteTools)
	}
}

func assertDiscoveredSession(t *testing.T, s Session, tool, title, cwd string, live bool) {
	t.Helper()
	if s.Tool != tool || s.Title != title || s.CWD != cwd || s.Live != live {
		t.Fatalf("unexpected %s session: %#v", tool, s)
	}
}

func containsString(values []string, want string) bool {
	i := sort.SearchStrings(values, want)
	return i < len(values) && values[i] == want
}
