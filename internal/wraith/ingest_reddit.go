package wraith

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/GetModus/wraith/internal/markdown"
)

// redditListing is the top-level Reddit API response.
type redditListing struct {
	Data struct {
		Children []struct {
			Kind string         `json:"kind"`
			Data redditPostData `json:"data"`
		} `json:"children"`
		After string `json:"after"`
	} `json:"data"`
}

type redditPostData struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Selftext    string  `json:"selftext"`
	URL         string  `json:"url"`
	Permalink   string  `json:"permalink"`
	Subreddit   string  `json:"subreddit"`
	Author      string  `json:"author"`
	Score       int     `json:"score"`
	NumComments int     `json:"num_comments"`
	CreatedUTC  float64 `json:"created_utc"`
	IsSelf      bool    `json:"is_self"`
}

// IngestReddit fetches Reddit saved posts via OAuth cookie and writes vault .md files.
func IngestReddit(vaultDir string, state *State, maxItems int) (int, error) {
	if maxItems <= 0 {
		maxItems = 40
	}

	// Extract Reddit token from Safari cookies
	cookies, err := ExtractCookies([]string{"reddit.com"}, "")
	if err != nil {
		return 0, fmt.Errorf("extract reddit cookies: %w", err)
	}

	token := cookies["token_v2"]
	if token == "" {
		return 0, fmt.Errorf("no Reddit token_v2 cookie — log into reddit.com in Safari first")
	}

	log.Println("wraith: fetching Reddit saved posts via OAuth cookie...")

	dir := filepath.Join(vaultDir, "brain", "reddit")
	os.MkdirAll(dir, 0755)

	client := &http.Client{Timeout: 30 * time.Second}
	authHeaders := map[string]string{
		"Authorization": "Bearer " + token,
		"User-Agent":    defaultUserAgent,
	}

	// First: get username via /api/v1/me
	username, err := redditGetUsername(client, authHeaders)
	if err != nil {
		return 0, fmt.Errorf("reddit username: %w", err)
	}
	log.Printf("wraith: Reddit username: %s", username)

	count := 0
	after := ""
	detailBudget := redditDetailBudget(maxItems)
	maxComments := redditMaxComments()

	for count < maxItems {
		url := fmt.Sprintf("https://oauth.reddit.com/user/%s/saved?limit=25&raw_json=1", username)
		if after != "" {
			url += "&after=" + after
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return count, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", defaultUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			return count, fmt.Errorf("reddit API: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return count, fmt.Errorf("reddit auth failed (%d) — token_v2 may be expired", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return count, fmt.Errorf("reddit API %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		var listing redditListing
		if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
			return count, fmt.Errorf("reddit JSON: %w", err)
		}

		if len(listing.Data.Children) == 0 {
			break
		}

		for _, child := range listing.Data.Children {
			if count >= maxItems {
				break
			}

			post := child.Data
			if post.ID == "" {
				continue
			}

			// Dedup
			if state.Exists("reddit-saved", post.ID) {
				continue
			}

			postURL := "https://www.reddit.com" + post.Permalink
			date := time.Now().Format("2006-01-02")
			slug := slugify(post.Title)
			if slug == "" {
				slug = "reddit-" + post.ID
			}
			filename := fmt.Sprintf("%s-%s.md", date, slug)
			path := filepath.Join(dir, filename)

			var detail *RedditDetailCapture
			if detailBudget > 0 {
				captured, err := captureRedditDetails(postURL, maxComments)
				if err != nil {
					log.Printf("wraith/reddit: detail capture failed for %s: %v", post.ID, err)
				} else {
					detail = captured
				}
				detailBudget--
			}

			var linked *FetchResult
			if !post.IsSelf && post.URL != "" {
				fetched, err := Fetch(post.URL, domainsForURL(post.URL))
				if err != nil {
					log.Printf("wraith/reddit: linked fetch failed for %s: %v", post.URL, err)
				} else {
					linked = fetched
				}
			}

			body := buildRedditBody(post, detail, linked)

			fm := map[string]interface{}{
				"source":     "reddit-saved",
				"subreddit":  post.Subreddit,
				"author":     post.Author,
				"url":        postURL,
				"score":      post.Score,
				"comments":   post.NumComments,
				"date":       date,
				"triage":     "pending",
				"has_detail": detail != nil,
			}
			if detail != nil {
				fm["captured_comments"] = detail.CommentCount
			}
			if !post.IsSelf && post.URL != "" {
				fm["external_url"] = post.URL
				if linked != nil {
					fm["linked_article_title"] = linked.Title
					fm["linked_article_status"] = linked.Status
				}
			}

			if err := markdown.Write(path, fm, body); err != nil {
				log.Printf("wraith: write error for reddit %s: %v", post.ID, err)
				continue
			}

			if err := state.Record("reddit-saved", post.ID, postURL, post.Title, path); err != nil {
				log.Printf("wraith: state record error: %v", err)
			}

			count++
			log.Printf("wraith: ingested reddit [%d] r/%s: %s", count, post.Subreddit, truncate(post.Title, 60))
		}

		after = listing.Data.After
		if after == "" {
			break
		}
	}

	log.Printf("wraith: Reddit — %d new saved posts", count)
	return count, nil
}

func redditGetUsername(client *http.Client, headers map[string]string) (string, error) {
	req, err := http.NewRequest("GET", "https://oauth.reddit.com/api/v1/me", nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reddit /me: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("reddit /me returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var me struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return "", fmt.Errorf("reddit /me JSON: %w", err)
	}
	if me.Name == "" {
		return "", fmt.Errorf("reddit /me: empty username")
	}
	return me.Name, nil
}
