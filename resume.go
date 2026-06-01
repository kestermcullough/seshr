package main

import (
	"fmt"
	"os"
	"syscall"
)

func resumeCommand(s Session) (string, []string, error) {
	switch s.Tool {
	case "claude":
		args := []string{"--resume", s.SessionUUID}
		if s.Live {
			// Claude expects the session id immediately after --resume.
			// --fork-session must be an additional flag.
			args = append(args, "--fork-session")
		}
		return "claude", args, nil
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
	path, err := resolveResumeBinary(bin)
	if err != nil {
		return fmt.Errorf("%s not on PATH: %w", bin, err)
	}
	if err := enterSessionCWD(s); err != nil {
		return err
	}
	argv := append([]string{path}, args...)
	return syscall.Exec(path, argv, os.Environ())
}

func enterSessionCWD(s Session) error {
	if s.CWD == "" {
		return nil
	}
	if err := changeWorkingDir(s.CWD); err != nil {
		return fmt.Errorf("change to session cwd %q: %w", s.CWD, err)
	}
	return nil
}

func resolveResumeBinary(bin string) (string, error) {
	if bin == "amp" {
		if p := findAmpBinary(); p != "" {
			return p, nil
		}
	}
	return lookPath(bin)
}
