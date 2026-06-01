package main

import (
	"os"
	"strings"
	"testing"
)

func TestResumeCommandClaudeLiveForkOrder(t *testing.T) {
	bin, args, err := resumeCommand(Session{
		Tool:        "claude",
		SessionUUID: "abc123",
		Live:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bin != "claude" {
		t.Fatalf("bin = %q", bin)
	}
	want := []string{"--resume", "abc123", "--fork-session"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
	}
}

func TestEnterSessionCWDChangesToRecordedSessionDir(t *testing.T) {
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	want := t.TempDir()
	if err := enterSessionCWD(Session{CWD: want}); err != nil {
		t.Fatal(err)
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
}

func TestEnterSessionCWDErrorsWhenRecordedDirIsUnavailable(t *testing.T) {
	missing := t.TempDir() + "/missing"
	err := enterSessionCWD(Session{CWD: missing})
	if err == nil {
		t.Fatal("expected error for missing cwd")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error %q does not mention missing cwd %q", err, missing)
	}
}
