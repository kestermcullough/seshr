package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Amp content cache: full thread exports live as JSON files under
// ~/.local/share/seshr/amp-cache/. We fetch lazily — on the first
// preview of each thread — and invalidate by comparing the file's mtime to
// the session's last_active (which comes from the API's `updated` field, so
// it advances whenever the thread changes server-side).

func ampCacheDir() string {
	return filepath.Join(dbDir(), "amp-cache")
}

func ampCachePath(uuid string) string {
	return filepath.Join(ampCacheDir(), "T-"+uuid+".json")
}

// ampThreadCached returns the cached export for s if present and at least
// as new as the session's last_active. Returns (nil, false) on cache miss
// or staleness.
func ampThreadCached(s Session) (*ampThread, bool) {
	cp := ampCachePath(s.SessionUUID)
	if !ampThreadFileFresh(cp, s.LastActive) {
		return nil, false
	}
	data, err := os.ReadFile(cp)
	if err != nil {
		return nil, false
	}
	var t ampThread
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// ampThreadFetch shells out to `amp threads export T-<uuid>`, persists the
// result to the cache, and returns the parsed thread.
func ampThreadFetch(s Session) (*ampThread, error) {
	bin := findAmpBinary()
	if bin == "" {
		return nil, fmt.Errorf("amp binary not on PATH")
	}
	out, err := runCommandOutput(bin, "threads", "export", "T-"+s.SessionUUID)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("amp threads export: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	var t ampThread
	if err := json.Unmarshal(out, &t); err != nil {
		return nil, err
	}
	cp := ampCachePath(s.SessionUUID)
	if err := os.MkdirAll(filepath.Dir(cp), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cp, out, 0o644); err != nil {
		return nil, err
	}
	if !s.LastActive.IsZero() {
		_ = os.Chtimes(cp, s.LastActive, s.LastActive)
	}
	return &t, nil
}

// needsAmpFetch reports whether s is a server-backed Amp session without a
// fresh local cache entry.
func needsAmpFetch(s Session) bool {
	if s.Tool != "amp" {
		return false
	}
	_, ok := ampThreadForPreview(s)
	return !ok
}

func ampThreadForPreview(s Session) (*ampThread, bool) {
	if s.Tool != "amp" {
		return nil, false
	}
	if s.FilePath != "" && ampThreadFileFresh(s.FilePath, s.LastActive) {
		if t, err := readAmpThreadFile(s.FilePath); err == nil {
			return t, true
		}
	}
	return ampThreadCached(s)
}

func ampThreadFileFresh(path string, lastActive time.Time) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return lastActive.IsZero() || !info.ModTime().Before(lastActive)
}

func readAmpThreadFile(path string) (*ampThread, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t ampThread
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
