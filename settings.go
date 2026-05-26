package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude's default cleanupPeriodDays (the value used if the key isn't present
// in settings.json). Confirmed via Anthropic's docs / source: 30 days.
const claudeDefaultCleanupPeriodDays = 30

// Threshold below which we surface a startup warning. Anything ≥ this and we
// assume the user has chosen their retention intentionally.
const claudeCleanupWarnThreshold = 90

func claudeSettingsPath() string {
	return filepath.Join(homeDir(), ".claude", "settings.json")
}

// ClaudeCleanupPeriodDays returns the effective value, whether it was
// explicitly set in settings.json (vs implicit default), and any read error.
func ClaudeCleanupPeriodDays() (days int, explicit bool, err error) {
	data, err := os.ReadFile(claudeSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return claudeDefaultCleanupPeriodDays, false, nil
		}
		return 0, false, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, false, err
	}
	if v, ok := raw["cleanupPeriodDays"]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true, nil
		case int:
			return n, true, nil
		}
	}
	return claudeDefaultCleanupPeriodDays, false, nil
}

// SetClaudeCleanupPeriodDays updates the value in ~/.claude/settings.json,
// preserving every other key in the file. Creates the file if missing.
func SetClaudeCleanupPeriodDays(days int) error {
	if days < 1 {
		return fmt.Errorf("cleanupPeriodDays must be at least 1")
	}
	path := claudeSettingsPath()
	raw := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse existing settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	raw["cleanupPeriodDays"] = days
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// ToolSetting is a per-tool retention summary suitable for printing or, later,
// rendering in a TUI screen.
type ToolSetting struct {
	Tool        string
	Description string
	Warning     string // empty unless the value is concerning
}

// SettingsReport returns a snapshot of each tool's session-retention posture.
func SettingsReport() []ToolSetting {
	var out []ToolSetting

	days, explicit, err := ClaudeCleanupPeriodDays()
	switch {
	case err != nil:
		out = append(out, ToolSetting{Tool: "claude", Description: "error reading settings: " + err.Error()})
	default:
		src := "default; not set"
		if explicit {
			src = "set in " + claudeSettingsPath()
		}
		t := ToolSetting{
			Tool:        "claude",
			Description: fmt.Sprintf("cleanupPeriodDays = %d (%s)", days, src),
		}
		if days < claudeCleanupWarnThreshold {
			t.Warning = fmt.Sprintf("session files older than %d days are auto-deleted on launch", days)
		}
		out = append(out, t)
	}

	out = append(out, ToolSetting{
		Tool:        "codex",
		Description: "no auto-cleanup — sessions persist indefinitely under ~/.codex/sessions/",
	})
	out = append(out, ToolSetting{
		Tool:        "amp",
		Description: "no client-side auto-cleanup; threads on ampcode.com persist until you `amp threads delete <id>`",
	})
	out = append(out, ToolSetting{
		Tool:        "pi",
		Description: "no auto-cleanup — sessions persist indefinitely under ~/.pi/agent/sessions/",
	})
	return out
}
