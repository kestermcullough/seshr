package main

import (
	"os"
	"path/filepath"
)

var (
	userHomeDir     = os.UserHomeDir
	seshrDataDir    string
	processIDIsLive = pidAlive
)

func homeDir() string {
	h, _ := userHomeDir()
	return h
}

func dataDir() string {
	if seshrDataDir != "" {
		return seshrDataDir
	}
	return filepath.Join(homeDir(), ".local", "share", "seshr")
}

func claudeProjectsDir() string { return filepath.Join(homeDir(), ".claude", "projects") }
func codexSessionsDir() string  { return filepath.Join(homeDir(), ".codex", "sessions") }
func ampThreadsDir() string     { return filepath.Join(homeDir(), ".local", "share", "amp", "threads") }
func piSessionsDir() string     { return filepath.Join(homeDir(), ".pi", "agent", "sessions") }
