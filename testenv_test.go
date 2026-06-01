package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func withIsolatedRuntime(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	oldHomeDir := userHomeDir
	oldDataDir := seshrDataDir
	oldProcessLive := processIDIsLive
	oldChangeWorkingDir := changeWorkingDir
	oldLookPath := lookPath
	oldRunCommandOutput := runCommandOutput

	userHomeDir = func() (string, error) { return home, nil }
	seshrDataDir = filepath.Join(home, "seshr-data")
	processIDIsLive = func(int) bool { return false }
	changeWorkingDir = os.Chdir
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	runCommandOutput = func(string, ...string) ([]byte, error) {
		return nil, errors.New("unexpected external command")
	}

	t.Cleanup(func() {
		userHomeDir = oldHomeDir
		seshrDataDir = oldDataDir
		processIDIsLive = oldProcessLive
		changeWorkingDir = oldChangeWorkingDir
		lookPath = oldLookPath
		runCommandOutput = oldRunCommandOutput
	})

	return home
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
