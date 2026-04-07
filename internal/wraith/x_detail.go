package wraith

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// XCommentSample captures one visible reply from the tweet detail page.
type XCommentSample struct {
	Index        int    `json:"index"`
	AuthorHandle string `json:"author_handle,omitempty"`
	Body         string `json:"body"`
}

// XDetailCapture holds the tweet detail extraction from Safari.
type XDetailCapture struct {
	Title          string           `json:"title"`
	Body           string           `json:"body"`
	CanonicalURL   string           `json:"canonical_url"`
	CommentCount   int              `json:"comment_count"`
	CommentSamples []XCommentSample `json:"comment_samples"`
	DetailCaptured bool             `json:"detail_captured"`
}

// LinkedArticleCapture holds fetched article content for one outbound link.
type LinkedArticleCapture struct {
	URL    string
	Title  string
	Text   string
	Status int
}

func xDetailBudget(maxItems int) int {
	if maxItems <= 0 {
		maxItems = 40
	}
	def := maxItems
	if def > 25 {
		def = 25
	}
	if def < 0 {
		def = 0
	}
	return envInt("WRAITH_X_DETAIL_COUNT", def)
}

func xMaxComments() int {
	return envInt("WRAITH_X_MAX_COMMENTS", 10)
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func captureXDetails(tweetURL string, maxComments int) (*XDetailCapture, error) {
	if !SafariAvailable() {
		return nil, fmt.Errorf("safari not available")
	}
	if err := SafariNavigateOpt(tweetURL, true); err != nil {
		return nil, err
	}

	time.Sleep(1500 * time.Millisecond)
	_ = SafariWaitForLoad(12 * time.Second)
	time.Sleep(1500 * time.Millisecond)

	js := fmt.Sprintf(`(function() {
	  const norm = (value) => (value || "").replace(/\s+/g, " ").trim();
	  const articles = [...document.querySelectorAll('article[data-testid="tweet"]')];
	  const primary = articles[0];
	  const title = primary ? norm([...primary.querySelectorAll('[data-testid="tweetText"]')].map((n) => n.innerText).join("\n")) : document.title;
	  const comments = articles.slice(1, %d + 1).map((article, index) => {
	    const text = norm([...article.querySelectorAll('[data-testid="tweetText"]')].map((n) => n.innerText).join("\n"));
	    let handle = null;
	    for (const anchor of article.querySelectorAll('a[href^="/"]')) {
	      const path = anchor.getAttribute("href") || "";
	      if (/^\/[A-Za-z0-9_]{1,15}$/.test(path)) {
	        handle = "@" + path.slice(1);
	        break;
	      }
	    }
	    return text ? { index: index + 1, author_handle: handle, body: text } : null;
	  }).filter(Boolean);
	  const detailText = primary ? norm(primary.innerText) : title;
	  return JSON.stringify({
	    title: title || document.title,
	    body: detailText || title || "",
	    canonical_url: location.href,
	    comment_count: comments.length,
	    comment_samples: comments,
	    detail_captured: Boolean(primary)
	  });
	})();`, maxComments)

	raw, err := SafariExecuteJS(js)
	if err != nil {
		return nil, err
	}

	raw = strings.TrimSpace(raw)
	var detail XDetailCapture
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return nil, fmt.Errorf("parse x detail: %w", err)
	}
	if detail.CanonicalURL == "" {
		detail.CanonicalURL = tweetURL
	}
	return &detail, nil
}

func captureLinkedArticles(urls []string) []LinkedArticleCapture {
	results := make([]LinkedArticleCapture, 0, len(urls))
	for _, link := range urls {
		link = strings.TrimSpace(link)
		if link == "" || isXURL(link) {
			continue
		}

		domains := domainsForURL(link)
		fetched, err := Fetch(link, domains)
		if err != nil {
			log.Printf("wraith/x: linked article fetch failed for %s: %v", truncate(link, 100), err)
			continue
		}

		text := strings.TrimSpace(fetched.Text)
		if len(text) > 4000 {
			text = text[:4000] + "\n[... truncated]"
		}

		results = append(results, LinkedArticleCapture{
			URL:    fetched.URL,
			Title:  fetched.Title,
			Text:   text,
			Status: fetched.Status,
		})
	}
	return results
}

func extractExpandedURLs(tweet birdTweet) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, u := range tweet.URLs {
		link := strings.TrimSpace(u.ExpandedURL)
		if link == "" || isXURL(link) {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		out = append(out, link)
	}
	return out
}

func domainsForURL(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "www.") {
		return []string{host, strings.TrimPrefix(host, "www.")}
	}
	return []string{host}
}

func isXURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.Contains(raw, "x.com") || strings.Contains(raw, "twitter.com") || strings.Contains(raw, "t.co/")
	}
	host := strings.ToLower(u.Hostname())
	return host == "x.com" || host == "www.x.com" || host == "twitter.com" || host == "www.twitter.com" || host == "t.co"
}

func buildXBody(tweet birdTweet, detail *XDetailCapture, links []string, articles []LinkedArticleCapture) string {
	var sb strings.Builder

	mainText := strings.TrimSpace(tweet.Text)
	if detail != nil && len(strings.TrimSpace(detail.Body)) > len(mainText) {
		mainText = strings.TrimSpace(detail.Body)
	}
	sb.WriteString(mainText)

	if len(links) > 0 {
		sb.WriteString("\n\nLinks:\n")
		for _, link := range links {
			sb.WriteString("- " + link + "\n")
		}
	}

	if len(tweet.Media) > 0 {
		var media []string
		for _, m := range tweet.Media {
			if m.VideoURL != "" {
				media = append(media, m.VideoURL)
			} else if m.URL != "" {
				media = append(media, m.URL)
			}
		}
		if len(media) > 0 {
			sb.WriteString("\nMedia:\n")
			for _, item := range media {
				sb.WriteString("- " + item + "\n")
			}
		}
	}

	if detail != nil && len(detail.CommentSamples) > 0 {
		sb.WriteString("\n## First Replies\n")
		for _, c := range detail.CommentSamples {
			line := strings.TrimSpace(c.Body)
			if line == "" {
				continue
			}
			if len(line) > 400 {
				line = line[:400] + "..."
			}
			if c.AuthorHandle != "" {
				sb.WriteString(fmt.Sprintf("%d. %s: %s\n", c.Index, c.AuthorHandle, line))
			} else {
				sb.WriteString(fmt.Sprintf("%d. %s\n", c.Index, line))
			}
		}
	}

	for _, article := range articles {
		sb.WriteString("\n## Linked Article\n")
		if article.Title != "" {
			sb.WriteString(article.Title + "\n")
		}
		sb.WriteString(article.URL + "\n\n")
		if article.Text != "" {
			sb.WriteString(article.Text + "\n")
		}
	}

	return strings.TrimSpace(sb.String())
}
