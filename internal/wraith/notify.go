package wraith

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// NotifyiMessage sends a message via macOS iMessage using AppleScript.
func NotifyiMessage(text, to string) error {
	if to == "" {
		to = "+18704160146" // General's phone
	}

	// Truncate for iMessage
	if len(text) > 800 {
		text = text[:800] + "..."
	}

	script := fmt.Sprintf(`tell application "Messages"
	set targetService to 1st account whose service type = iMessage
	set targetBuddy to participant "%s" of targetService
	send "%s" to targetBuddy
end tell`, to, escapeAppleScript(text))

	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("wraith/notify: iMessage failed: %s %v", string(out), err)
		return err
	}
	return nil
}

// NotifyIngestion sends a triage summary via iMessage.
func NotifyIngestion(source string, results []TriageResult) {
	if len(results) == 0 {
		return
	}

	var adapt, keep, discard []TriageResult
	for _, r := range results {
		switch r.Class {
		case "ADAPT":
			adapt = append(adapt, r)
		case "KEEP":
			keep = append(keep, r)
		case "DISCARD":
			discard = append(discard, r)
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "WRAITH: %d %s items ingested\n\n", len(results), source)

	if len(adapt) > 0 {
		fmt.Fprintf(&sb, "ADAPT (%d):\n", len(adapt))
		for _, r := range adapt {
			fmt.Fprintf(&sb, "  - %s\n    %s\n", truncate(r.Title, 60), truncate(r.Reason, 80))
		}
		sb.WriteString("\n")
	}
	if len(keep) > 0 {
		fmt.Fprintf(&sb, "KEEP (%d):\n", len(keep))
		for _, r := range keep[:min(5, len(keep))] {
			fmt.Fprintf(&sb, "  - %s\n", truncate(r.Title, 60))
		}
		sb.WriteString("\n")
	}
	if len(discard) > 0 {
		fmt.Fprintf(&sb, "DISCARD (%d)\n", len(discard))
	}

	if err := NotifyiMessage(sb.String(), ""); err != nil {
		log.Printf("wraith/notify: failed to send summary: %v", err)
	}
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
