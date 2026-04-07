package wraith

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GetModus/wraith/internal/markdown"
)

// birdTweet is the JSON structure returned by bird CLI.
type birdTweet struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
	Author    struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
	URLs []struct {
		ExpandedURL string `json:"expanded_url"`
	} `json:"urls"`
	Media []struct {
		Type     string `json:"type"`
		URL      string `json:"url"`
		VideoURL string `json:"videoUrl"`
	} `json:"media"`
	LikeCount    int    `json:"likeCount"`
	RetweetCount int    `json:"retweetCount"`
	ReplyCount   int    `json:"replyCount"`
	ConvID       string `json:"conversationId"`
}

// BirdAvailable checks if the bird CLI is installed.
func BirdAvailable() bool {
	_, err := exec.LookPath("bird")
	return err == nil
}

// IngestX fetches X bookmarks via bird CLI, deduplicates, and writes vault .md files.
func IngestX(vaultDir string, state *State, maxItems int) (int, error) {
	if !BirdAvailable() {
		return 0, fmt.Errorf("bird CLI not installed — run: npm install -g @steipete/bird")
	}

	if maxItems <= 0 {
		maxItems = 40
	}

	log.Println("wraith: fetching X bookmarks via bird CLI...")

	// Use --all for comprehensive ingestion, --count for smaller runs
	var args []string
	if maxItems >= 200 {
		args = []string{"bookmarks", "--cookie-source", "safari", "--json", "--plain", "--all"}
	} else {
		args = []string{"bookmarks", "--cookie-source", "safari", "--json", "--plain", "--count", fmt.Sprintf("%d", maxItems)}
	}
	cmd := exec.Command("bird", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("bird failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return 0, fmt.Errorf("bird exec: %w", err)
	}

	// bird returns bare array for --count, but {"tweets": [...]} for --all
	var tweets []birdTweet
	if err := json.Unmarshal(out, &tweets); err != nil {
		// Try wrapped format
		var wrapped struct {
			Tweets []birdTweet `json:"tweets"`
		}
		if err2 := json.Unmarshal(out, &wrapped); err2 != nil {
			return 0, fmt.Errorf("bird JSON parse: %w", err)
		}
		tweets = wrapped.Tweets
	}

	log.Printf("wraith: bird returned %d bookmarks", len(tweets))

	dir := filepath.Join(vaultDir, "brain", "x")
	os.MkdirAll(dir, 0755)

	count := 0
	detailBudget := xDetailBudget(maxItems)
	maxComments := xMaxComments()
	for _, tweet := range tweets {
		if tweet.ID == "" || tweet.Text == "" {
			continue
		}

		// Dedup via state table
		if state.Exists("x-bookmarks", tweet.ID) {
			continue
		}

		url := fmt.Sprintf("https://x.com/%s/status/%s", tweet.Author.Username, tweet.ID)
		title := firstLine(tweet.Text, 120)
		date := time.Now().Format("2006-01-02")
		slug := slugify(title)
		if slug == "" {
			slug = "tweet-" + tweet.ID
		}
		filename := fmt.Sprintf("%s-%s.md", date, slug)
		path := filepath.Join(dir, filename)

		links := extractExpandedURLs(tweet)

		var detail *XDetailCapture
		if detailBudget > 0 {
			captured, err := captureXDetails(url, maxComments)
			if err != nil {
				log.Printf("wraith/x: detail capture failed for %s: %v", tweet.ID, err)
			} else {
				detail = captured
			}
			detailBudget--
		}

		articles := captureLinkedArticles(links)
		body := buildXBody(tweet, detail, links, articles)

		// Write vault file
		fm := map[string]interface{}{
			"source":          "x-bookmark",
			"author":          fmt.Sprintf("@%s (%s)", tweet.Author.Username, tweet.Author.Name),
			"url":             url,
			"date":            date,
			"triage":          "pending",
			"likes":           tweet.LikeCount,
			"retweets":        tweet.RetweetCount,
			"replies":         tweet.ReplyCount,
			"conversation_id": tweet.ConvID,
			"linked_urls":     links,
			"has_detail":      detail != nil,
		}
		if detail != nil {
			fm["captured_comments"] = detail.CommentCount
		}
		if len(articles) > 0 {
			fm["has_linked_article"] = true
			fm["linked_article_url"] = articles[0].URL
			if articles[0].Title != "" {
				fm["linked_article_title"] = articles[0].Title
			}
		}

		if err := markdown.Write(path, fm, body); err != nil {
			log.Printf("wraith: write error for tweet %s: %v", tweet.ID, err)
			continue
		}

		// Record in state
		if err := state.Record("x-bookmarks", tweet.ID, url, title, path); err != nil {
			log.Printf("wraith: state record error: %v", err)
		}

		count++
		log.Printf("wraith: ingested [%d] @%s: %s", count, tweet.Author.Username, truncate(title, 60))
	}

	log.Printf("wraith: X bookmarks — %d new, %d total from bird", count, len(tweets))
	return count, nil
}

func firstLine(s string, maxLen int) string {
	line := strings.SplitN(s, "\n", 2)[0]
	line = strings.TrimSpace(line)
	if len(line) > maxLen {
		line = line[:maxLen]
	}
	return line
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
