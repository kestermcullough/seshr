package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		all      = flag.Bool("all", false, "show sessions from every cwd (default: only current dir + descendants)")
		archived = flag.Bool("archived", false, "include archived sessions")
		listOnly = flag.Bool("list", false, "dump sessions to stdout and exit (no TUI)")
	)
	flag.Parse()

	db, err := OpenDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}

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

	if *listOnly {
		db.Close()
		printSessions(sessions, filter)
		return
	}

	resume, err := runTUI(db, sessions, filter)
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
