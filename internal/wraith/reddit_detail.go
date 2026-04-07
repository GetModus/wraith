package wraith

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RedditCommentSample captures one visible comment from a Reddit thread page.
type RedditCommentSample struct {
	Index        int    `json:"index"`
	AuthorHandle string `json:"author_handle,omitempty"`
	Body         string `json:"body"`
}

// RedditDetailCapture holds detailed extraction for one saved Reddit item.
type RedditDetailCapture struct {
	Title          string                `json:"title"`
	Body           string                `json:"body"`
	CanonicalURL   string                `json:"canonical_url"`
	CommentCount   int                   `json:"comment_count"`
	CommentSamples []RedditCommentSample `json:"comment_samples"`
	DetailCaptured bool                  `json:"detail_captured"`
}

func redditDetailBudget(maxItems int) int {
	if maxItems <= 0 {
		maxItems = 40
	}
	def := maxItems
	if def > 25 {
		def = 25
	}
	return envInt("WRAITH_REDDIT_DETAIL_COUNT", def)
}

func redditMaxComments() int {
	return envInt("WRAITH_REDDIT_MAX_COMMENTS", 10)
}

func captureRedditDetails(detailURL string, maxComments int) (*RedditDetailCapture, error) {
	if !SafariAvailable() {
		return nil, fmt.Errorf("safari not available")
	}
	if err := SafariNavigate(detailURL); err != nil {
		return nil, err
	}

	time.Sleep(1500 * time.Millisecond)
	_ = SafariWaitForLoad(12 * time.Second)
	time.Sleep(1200 * time.Millisecond)

	js := fmt.Sprintf(`(function() {
	  const norm = (value) => (value || "").replace(/\s+/g, " ").trim();
	  const titleNode = document.querySelector('a.title, .top-matter .title');
	  const bodyNode = document.querySelector('.expando .usertext-body .md, .entry .usertext-body .md, .entry .md');
	  const comments = [...document.querySelectorAll('.comment .entry .md')]
	    .map((node, index) => {
	      const body = norm(node.innerText);
	      if (!body) return null;
	      const root = node.closest('.comment');
	      const author = root ? root.querySelector('.author') : null;
	      return {
	        index: index + 1,
	        author_handle: author ? "u/" + norm(author.textContent).replace(/^u\//, "") : null,
	        body
	      };
	    })
	    .filter(Boolean)
	    .slice(0, %d);
	  const detailTitle = norm(titleNode ? titleNode.textContent : document.title);
	  const detailBody = norm(bodyNode ? bodyNode.innerText : detailTitle);
	  return JSON.stringify({
	    title: detailTitle || document.title,
	    body: detailBody || detailTitle || "",
	    canonical_url: location.href,
	    comment_count: comments.length,
	    comment_samples: comments,
	    detail_captured: true
	  });
	})();`, maxComments)

	raw, err := SafariExecuteJS(js)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)

	var detail RedditDetailCapture
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return nil, fmt.Errorf("parse reddit detail: %w", err)
	}
	if detail.CanonicalURL == "" {
		detail.CanonicalURL = detailURL
	}
	return &detail, nil
}

func buildRedditBody(post redditPostData, detail *RedditDetailCapture, linked *FetchResult) string {
	var sb strings.Builder

	title := strings.TrimSpace(post.Title)
	if title != "" {
		sb.WriteString(title)
	}

	mainBody := strings.TrimSpace(post.Selftext)
	if detail != nil && strings.TrimSpace(detail.Body) != "" && len(strings.TrimSpace(detail.Body)) > len(mainBody) {
		mainBody = strings.TrimSpace(detail.Body)
	}
	if mainBody != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(mainBody)
	}

	if !post.IsSelf && post.URL != "" {
		sb.WriteString("\n\nExternal link: " + post.URL)
	}

	if detail != nil && len(detail.CommentSamples) > 0 {
		sb.WriteString("\n\n## First Comments\n")
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

	if linked != nil && strings.TrimSpace(linked.Text) != "" {
		sb.WriteString("\n\n## Linked Article\n")
		if linked.Title != "" {
			sb.WriteString(linked.Title + "\n")
		}
		sb.WriteString(linked.URL + "\n\n")
		text := strings.TrimSpace(linked.Text)
		if len(text) > 4000 {
			text = text[:4000] + "\n[... truncated]"
		}
		sb.WriteString(text)
	}

	return strings.TrimSpace(sb.String())
}
