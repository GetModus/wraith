package wraith

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const safariTimeout = 15 * time.Second

// SafariResult holds extracted page data.
type SafariResult struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Text     string `json:"text"`
	Selected string `json:"selected,omitempty"`
	Error    string `json:"error,omitempty"`
}

// SafariTab represents an open Safari tab.
type SafariTab struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Window int    `json:"window"`
}

// SafariAvailable checks if Safari is running and accessible.
func SafariAvailable() bool {
	out, err := osascript(`tell application "System Events" to (name of processes) contains "Safari"`)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// SafariNavigate opens a URL in Safari without stealing focus.
// Set background=true to keep the current app in front.
func SafariNavigate(url string) error {
	return SafariNavigateOpt(url, true)
}

// SafariNavigateOpt opens a URL in Safari. If background is true, Safari
// will not be activated (no focus steal).
func SafariNavigateOpt(url string, background bool) error {
	escaped := strings.ReplaceAll(url, `"`, `\"`)
	var script string
	if background {
		script = fmt.Sprintf(`tell application "Safari"
	if (count of windows) = 0 then
		make new document with properties {URL:"%s"}
	else
		set URL of current tab of front window to "%s"
	end if
end tell`, escaped, escaped)
	} else {
		script = fmt.Sprintf(`tell application "Safari"
	activate
	if (count of windows) = 0 then
		make new document with properties {URL:"%s"}
	else
		set URL of current tab of front window to "%s"
	end if
end tell`, escaped, escaped)
	}

	_, err := osascriptRaw(script)
	if err != nil {
		return fmt.Errorf("safari navigate: %w", err)
	}
	log.Printf("wraith/safari: navigated to %s (background=%v)", truncate(url, 80), background)
	return nil
}

// SafariGetURL returns the URL of the current tab.
func SafariGetURL() string {
	out, err := osascript(`tell application "Safari" to return URL of current tab of front window`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SafariGetTitle returns the title of the current tab.
func SafariGetTitle() string {
	out, err := osascript(`tell application "Safari" to return name of current tab of front window`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SafariExecuteJS runs JavaScript in the current Safari tab and returns the result.
func SafariExecuteJS(js string) (string, error) {
	// For complex JS, use temp file approach to avoid shell escaping issues
	escaped := escapeForAppleScript(js)
	script := fmt.Sprintf(`tell application "Safari"
	do JavaScript %s in current tab of front window
end tell`, escaped)

	out, err := osascriptRaw(script)
	if err != nil {
		return "", fmt.Errorf("safari JS: %w", err)
	}
	return out, nil
}

// SafariExtract extracts page content — title, URL, and body text.
// If selector is non-empty, also extracts text from that CSS selector.
func SafariExtract(selector string) (*SafariResult, error) {
	js := `(function() {
	var r = {};
	r.title = document.title;
	r.url = window.location.href;
	r.text = document.body ? document.body.innerText.substring(0, 50000) : '';`

	if selector != "" {
		escaped := strings.ReplaceAll(selector, `'`, `\'`)
		js += fmt.Sprintf(`
	var el = document.querySelector('%s');
	r.selected = el ? el.innerText.substring(0, 10000) : '';`, escaped)
	}

	js += `
	return JSON.stringify(r);
})();`

	raw, err := SafariExecuteJS(js)
	if err != nil {
		return nil, err
	}

	raw = strings.TrimSpace(raw)
	var result SafariResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return &SafariResult{Text: raw, Error: "JSON parse failed"}, nil
	}
	return &result, nil
}

// SafariExtractLinks extracts all links from the current page.
// Optional filterPattern filters href by substring match.
func SafariExtractLinks(filterPattern string) ([]map[string]string, error) {
	js := `(function() {
	var links = Array.from(document.querySelectorAll('a[href]'));
	var result = links.map(function(a) {
		return {href: a.href, text: (a.innerText || '').substring(0, 200), title: a.title || ''};
	});
	return JSON.stringify(result);
})();`

	raw, err := SafariExecuteJS(js)
	if err != nil {
		return nil, err
	}

	raw = strings.TrimSpace(raw)
	var links []map[string]string
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return nil, fmt.Errorf("parse links: %w", err)
	}

	if filterPattern != "" {
		var filtered []map[string]string
		for _, l := range links {
			if strings.Contains(l["href"], filterPattern) {
				filtered = append(filtered, l)
			}
		}
		return filtered, nil
	}

	return links, nil
}

// SafariClick clicks an element matching the CSS selector.
func SafariClick(selector string) (bool, error) {
	escaped := strings.ReplaceAll(selector, `'`, `\'`)
	js := fmt.Sprintf(`(function() {
	var el = document.querySelector('%s');
	if (el) { el.click(); return 'clicked'; }
	return 'not_found';
})();`, escaped)

	raw, err := SafariExecuteJS(js)
	if err != nil {
		return false, err
	}
	return strings.Contains(raw, "clicked"), nil
}

// SafariScroll scrolls the page. direction: "down", "up", "top", "bottom".
func SafariScroll(direction string, amount int) error {
	if amount <= 0 {
		amount = 500
	}
	var js string
	switch direction {
	case "up":
		js = fmt.Sprintf("window.scrollBy(0, -%d);", amount)
	case "top":
		js = "window.scrollTo(0, 0);"
	case "bottom":
		js = "window.scrollTo(0, document.body.scrollHeight);"
	default: // "down"
		js = fmt.Sprintf("window.scrollBy(0, %d);", amount)
	}
	_, err := SafariExecuteJS(js)
	return err
}

// SafariGetTabs lists all open Safari tabs across all windows.
func SafariGetTabs() ([]SafariTab, error) {
	script := `tell application "Safari"
	set output to ""
	repeat with w in windows
		set winIdx to index of w
		repeat with t in tabs of w
			set tabUrl to URL of t
			set tabName to name of t
			set output to output & winIdx & "	" & tabUrl & "	" & tabName & linefeed
		end repeat
	end repeat
	return output
end tell`

	raw, err := osascriptRaw(script)
	if err != nil {
		return nil, fmt.Errorf("safari tabs: %w", err)
	}

	var tabs []SafariTab
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		tab := SafariTab{URL: parts[1]}
		if len(parts) >= 3 {
			tab.Title = parts[2]
		}
		fmt.Sscanf(parts[0], "%d", &tab.Window)
		tabs = append(tabs, tab)
	}
	return tabs, nil
}

// SafariWaitForLoad waits for the page to finish loading (up to timeout).
func SafariWaitForLoad(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		js := `document.readyState`
		out, err := SafariExecuteJS(js)
		if err == nil && strings.Contains(out, "complete") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("safari: page load timeout after %v", timeout)
}

// SafariFetch navigates to a URL, waits for load, and extracts the page content.
// This is the high-level "browse and read" operation.
func SafariFetch(url string) (*SafariResult, error) {
	if !SafariAvailable() {
		return nil, fmt.Errorf("safari is not running")
	}

	if err := SafariNavigate(url); err != nil {
		return nil, err
	}

	// Brief pause to let Safari initiate navigation before we poll readyState
	time.Sleep(1 * time.Second)

	// Wait for the URL to change (confirms navigation started)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		currentURL := SafariGetURL()
		if strings.Contains(currentURL, strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")[:20]) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Wait for page to finish loading
	if err := SafariWaitForLoad(20 * time.Second); err != nil {
		log.Printf("wraith/safari: load timeout for %s — extracting anyway", truncate(url, 60))
	}

	return SafariExtract("")
}

// SafariScreenshot captures the Safari window to a temp PNG file.
// Returns the file path on success.
func SafariScreenshot() (string, error) {
	tmpFile, err := os.CreateTemp("", "safari-screenshot-*.png")
	if err != nil {
		return "", err
	}
	tmpFile.Close()
	path := tmpFile.Name()

	// screencapture -l <windowID> captures a specific window
	// First get Safari's window ID
	winIDScript := `tell application "System Events" to tell process "Safari" to return id of front window`
	winID, err := osascript(winIDScript)
	if err != nil {
		// Fallback: capture frontmost window
		cmd := exec.Command("screencapture", "-o", "-x", "-l", "1", path)
		cmd.Run()
	} else {
		winID = strings.TrimSpace(winID)
		cmd := exec.Command("screencapture", "-o", "-x", "-l", winID, path)
		cmd.Run()
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		os.Remove(path)
		return "", fmt.Errorf("screenshot failed")
	}

	return path, nil
}

// ---- internal ----

// osascript runs a one-line AppleScript.
func osascript(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), safariTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript: %s — %w", truncate(string(out), 100), err)
	}
	return string(out), nil
}

// osascriptRaw runs a multi-line AppleScript via temp file.
func osascriptRaw(script string) (string, error) {
	tmpFile, err := os.CreateTemp("", "wraith-safari-*.scpt")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString(script)
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), safariTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", tmpFile.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript: %s — %w", truncate(string(out), 100), err)
	}
	return string(out), nil
}

// escapeForAppleScript prepares a string for embedding in AppleScript.
func escapeForAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}
