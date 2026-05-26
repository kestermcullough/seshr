package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

func main() {
	// Bare subcommand: `seshr settings ...`. Handled before flag.Parse so the
	// subcommand args don't have to fight with the TUI's top-level flags.
	if len(os.Args) > 1 && os.Args[1] == "settings" {
		runSettingsCommand(os.Args[2:])
		return
	}

	var (
		all      = flag.Bool("all", false, "(--list only) show sessions from every cwd")
		archived = flag.Bool("archived", false, "(--list only) include archived sessions")
		listOnly = flag.Bool("list", false, "dump sessions to stdout and exit (no TUI)")
	)
	flag.Parse()

	// Surface a warning if Claude's cleanup is set low. Print to stderr so it
	// stays visible after the alt-screen TUI exits.
	if days, _, err := ClaudeCleanupPeriodDays(); err == nil && days < claudeCleanupWarnThreshold {
		fmt.Fprintf(os.Stderr,
			"⚠ Claude cleanupPeriodDays=%d — older session files get auto-deleted.\n"+
				"  Run: seshr settings claude-cleanup-days 365   (or higher)\n\n",
			days)
	}

	db, err := OpenDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}

	if *listOnly {
		discovered, errs := DiscoverAll()
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "warn:", e)
		}
		if err := db.SyncSessions(discovered); err != nil {
			fmt.Fprintln(os.Stderr, "sync:", err)
			os.Exit(1)
		}
		filter := QueryFilter{ShowArchived: *archived}
		if !*all {
			if cwd, err := os.Getwd(); err == nil {
				filter.CWDPrefix = cwd
			}
		}
		sessions, err := db.Query(filter)
		if err != nil {
			fmt.Fprintln(os.Stderr, "query:", err)
			os.Exit(1)
		}
		db.Close()
		printSessions(sessions, filter)
		return
	}

	// TUI mode: skip the initial bulk discovery. The TUI re-discovers
	// every time a project is entered, so we'd just be duplicating work
	// the first time. Subsequent launches see whatever the previous TUI
	// sync left in the DB until the user re-enters a project.
	resume, err := runTUI(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		db.Close()
		os.Exit(1)
	}
	if resume != nil {
		_ = db.MarkOpened(resume.ID())
		db.Close()
		if err := resumeSession(*resume); err != nil {
			fmt.Fprintln(os.Stderr, "resume:", err)
			os.Exit(1)
		}
	} else {
		db.Close()
	}
}

func printSessions(sessions []Session, f QueryFilter) {
	scopeNote := "(all cwds)"
	if f.CWDPrefix != "" {
		scopeNote = "scope=" + f.CWDPrefix
	}
	fmt.Printf("Showing %d sessions  %s\n\n", len(sessions), scopeNote)

	byTool := map[string]int{}
	for _, s := range sessions {
		byTool[s.Tool]++
	}
	fmt.Printf("breakdown: claude=%d codex=%d amp=%d pi=%d\n\n",
		byTool["claude"], byTool["codex"], byTool["amp"], byTool["pi"])

	for _, s := range sessions {
		cwd := s.CWD
		if cwd == "" {
			cwd = "?"
		}
		title := s.Title
		if title == "" {
			title = "(no title)"
		}
		when := s.LastActive.Format("2006-01-02 15:04")
		fmt.Printf("%-6s %s %7s  [%s] %s\n", s.Tool, when, humanSize(s.Size), s.TitleSource, title)
		fmt.Printf("       cwd=%s\n       id=%s\n", cwd, s.SessionUUID)
	}
}

// runSettingsCommand handles the `seshr settings ...` subcommand. With no
// further args it prints a per-tool retention report. The only mutating
// sub-sub-command today is `claude-cleanup-days [N]` (omit N to just show).
func runSettingsCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Session retention by tool:\n")
		for _, s := range SettingsReport() {
			fmt.Printf("  %-6s %s\n", s.Tool, s.Description)
			if s.Warning != "" {
				fmt.Printf("         ⚠ %s\n", s.Warning)
			}
		}
		fmt.Println()
		fmt.Println("Update Claude's value:")
		fmt.Println("  seshr settings claude-cleanup-days <days>")
		fmt.Println()
		fmt.Println("Suggested: 365 (one year) or 99999 (effectively forever).")
		return
	}
	switch args[0] {
	case "claude-cleanup-days":
		if len(args) == 1 {
			days, explicit, err := ClaudeCleanupPeriodDays()
			if err != nil {
				fmt.Fprintln(os.Stderr, "read failed:", err)
				os.Exit(1)
			}
			note := "default (not set)"
			if explicit {
				note = "set"
			}
			fmt.Printf("%d  (%s, %s)\n", days, claudeSettingsPath(), note)
			return
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "expected integer days, got:", args[1])
			os.Exit(2)
		}
		if err := SetClaudeCleanupPeriodDays(n); err != nil {
			fmt.Fprintln(os.Stderr, "set failed:", err)
			os.Exit(1)
		}
		fmt.Printf("set claude cleanupPeriodDays = %d in %s\n", n, claudeSettingsPath())
	default:
		fmt.Fprintln(os.Stderr, "unknown settings subcommand:", args[0])
		fmt.Fprintln(os.Stderr, "valid: claude-cleanup-days [<days>]")
		os.Exit(2)
	}
}

func humanSize(b int64) string {
	switch {
	case b <= 0:
		return "-"
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(b)/(1024*1024))
	}
}
