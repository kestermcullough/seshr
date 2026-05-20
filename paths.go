package main

import (
	"os"
	"path/filepath"
)

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func claudeProjectsDir() string { return filepath.Join(homeDir(), ".claude", "projects") }
func codexSessionsDir() string  { return filepath.Join(homeDir(), ".codex", "sessions") }
func ampThreadsDir() string     { return filepath.Join(homeDir(), ".local", "share", "amp", "threads") }
func piSessionsDir() string     { return filepath.Join(homeDir(), ".pi", "agent", "sessions") }
