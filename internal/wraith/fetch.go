package wraith

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"

// FetchResult holds the output of a URL fetch.
type FetchResult struct {
	URL     string
	Title   string
	Text    string
	RawHTML string
	Status  int
}

// Fetch retrieves a URL and extracts readable text.
// If domains are provided, Safari cookies for those domains are injected.
func Fetch(url string, domains []string) (*FetchResult, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Inject cookies if domains provided
	if len(domains) > 0 {
		cookies, err := ExtractCookies(domains, "")
		if err == nil && len(cookies) > 0 {
			req.Header.Set("Cookie", CookieHeader(cookies))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB max
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	html := string(body)
	title := extractTitle(html)
	text := htmlToText(html)

	return &FetchResult{
		URL:     resp.Request.URL.String(),
		Title:   title,
		Text:    text,
		RawHTML: html,
		Status:  resp.StatusCode,
	}, nil
}

func extractTitle(html string) string {
	re := regexp.MustCompile(`(?si)<title[^>]*>(.*?)</title>`)
	m := re.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// htmlToText strips HTML tags and extracts readable text.
// This is intentionally simple — not a full readability implementation.
func htmlToText(html string) string {
	// Remove script and style blocks
	reScript := regexp.MustCompile(`(?si)<script[^>]*>.*?</script>`)
	html = reScript.ReplaceAllString(html, "")
	reStyle := regexp.MustCompile(`(?si)<style[^>]*>.*?</style>`)
	html = reStyle.ReplaceAllString(html, "")

	// Remove nav, header, footer
	reNav := regexp.MustCompile(`(?si)<(?:nav|header|footer)[^>]*>.*?</(?:nav|header|footer)>`)
	html = reNav.ReplaceAllString(html, "")

	// Convert <br>, <p>, <div>, <li> to newlines
	reBlock := regexp.MustCompile(`(?i)<(?:br|/p|/div|/li|/h[1-6])[^>]*>`)
	html = reBlock.ReplaceAllString(html, "\n")

	// Strip all remaining tags
	reTags := regexp.MustCompile(`<[^>]+>`)
	text := reTags.ReplaceAllString(html, "")

	// Decode common entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Collapse whitespace
	reSpaces := regexp.MustCompile(`[ \t]+`)
	text = reSpaces.ReplaceAllString(text, " ")
	reLines := regexp.MustCompile(`\n{3,}`)
	text = reLines.ReplaceAllString(text, "\n\n")

	text = strings.TrimSpace(text)

	// Truncate
	if len(text) > 50000 {
		text = text[:50000] + "\n[... truncated]"
	}

	return text
}

// slugify lives here to be shared across the package.
// It's also defined in worker/ingestion.go but that's a different package.
func slugify(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	re := regexp.MustCompile(`[^\w\s-]`)
	text = re.ReplaceAllString(text, "")
	text = regexp.MustCompile(`[\s_]+`).ReplaceAllString(text, "-")
	text = regexp.MustCompile(`-+`).ReplaceAllString(text, "-")
	if len(text) > 80 {
		text = text[:80]
	}
	return strings.TrimRight(text, "-")
}
