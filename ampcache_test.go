package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNeedsAmpFetchHonorsLocalFileFreshness(t *testing.T) {
	withIsolatedRuntime(t)

	path := filepath.Join(t.TempDir(), "T-thread.json")
	if err := os.WriteFile(path, []byte(`{"id":"T-thread","messages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := time.Unix(200, 0)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}

	fresh := Session{
		Tool:        "amp",
		SessionUUID: "thread",
		FilePath:    path,
		LastActive:  time.Unix(100, 0),
	}
	if needsAmpFetch(fresh) {
		t.Fatal("fresh local Amp file should not need fetch")
	}

	stale := fresh
	stale.LastActive = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	if !needsAmpFetch(stale) {
		t.Fatal("stale local Amp file should need fetch")
	}
}

func TestAmpThreadFetchUsesInjectedCommandAndDataDir(t *testing.T) {
	withIsolatedRuntime(t)

	lookPath = func(name string) (string, error) {
		if name == "amp" {
			return "/fake/amp", nil
		}
		return "", exec.ErrNotFound
	}
	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		if name != "/fake/amp" || strings.Join(args, " ") != "threads export T-thread" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte(`{"id":"T-thread","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`), nil
	}

	lastActive := time.Unix(100, 0)
	thread, err := ampThreadFetch(Session{
		Tool:        "amp",
		SessionUUID: "thread",
		LastActive:  lastActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "T-thread" {
		t.Fatalf("thread id = %q", thread.ID)
	}
	info, err := os.Stat(ampCachePath("thread"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(lastActive) {
		t.Fatalf("cache mtime = %s, want %s", info.ModTime(), lastActive)
	}
}
