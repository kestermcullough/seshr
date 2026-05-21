package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func resumeCommand(s Session) (string, []string, error) {
	switch s.Tool {
	case "claude":
		return "claude", []string{"--resume", s.SessionUUID}, nil
	case "codex":
		return "codex", []string{"resume", s.SessionUUID}, nil
	case "amp":
		return "amp", []string{"threads", "continue", "T-" + s.SessionUUID}, nil
	case "pi":
		return "pi", []string{"--session", s.SessionUUID}, nil
	}
	return "", nil, fmt.Errorf("unknown tool %q", s.Tool)
}

// resumeSession replaces the current process with the native tool's CLI,
// dropping the user into the resumed session. On success it does not return.
func resumeSession(s Session) error {
	bin, args, err := resumeCommand(s)
	if err != nil {
		return err
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%s not on PATH: %w", bin, err)
	}
	argv := append([]string{path}, args...)
	return syscall.Exec(path, argv, os.Environ())
}
