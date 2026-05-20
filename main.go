package main

import (
	"fmt"
	"os"
)

func main() {
	sessions, errs := DiscoverAll()
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, "warn:", err)
	}
	fmt.Printf("Found %d sessions\n\n", len(sessions))

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
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1fK", float64(b)/1024)
	}
	return fmt.Sprintf("%.1fM", float64(b)/(1024*1024))
}
