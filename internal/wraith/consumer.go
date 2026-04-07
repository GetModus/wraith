package wraith

import (
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ingestYouTubeVideoFn = IngestYouTubeVideo
	ingestYouTubeFn      = IngestYouTube
)

// ProcessQueue reads pending captures from the queue, deduplicates against
// the wraith state, writes vault markdown files, and marks items done.
func ProcessQueue(queue *Queue, state *State, vaultDir string, limit int) (int, error) {
	return ProcessQueueWithOfficers(queue, state, vaultDir, limit, defaultOfficerPipeline())
}

// ProcessQueueWithOfficers routes intake through Scout and enforces Librarian-only filing.
func ProcessQueueWithOfficers(queue *Queue, state *State, vaultDir string, limit int, officers OfficerPipeline) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	if officers.Scout == nil || officers.Librarian == nil {
		officers = defaultOfficerPipeline()
	}

	pending := queue.Pending(limit)
	if len(pending) == 0 {
		return 0, nil
	}

	log.Printf("wraith/consumer: processing %d queued captures", len(pending))

	count := 0
	dataDir := filepath.Dir(queue.path)
	for _, cap := range pending {
		queue.SetStatus(cap.ID, "processing", "")

		if handled, err := processDirectYouTubeCapture(queue, state, vaultDir, cap); handled {
			if err != nil {
				queue.SetStatus(cap.ID, "failed", err.Error())
				log.Printf("wraith/consumer: youtube route failed for %s: %v", cap.ID, err)
				continue
			}
			count++
			continue
		}

		// Dedup by URL
		source := captureSource(cap)
		externalID := captureExternalID(cap)
		if state.Exists(source, externalID) {
			queue.SetStatus(cap.ID, "deduped", "duplicate of previously ingested state record")
			log.Printf("wraith/consumer: skip duplicate %s — %s", cap.ID, truncate(cap.Title, 50))
			continue
		}

		assessment := officers.Scout.Assess(cap)
		queue.SetStatus(cap.ID, "triaged", assessment.Class+": "+assessment.Reason)
		addTransition(cap, "triaged", "scout:"+assessment.Class+" "+assessment.Reason)
		if assessment.Class == "mission_candidate" {
			addTransition(cap, "mission_candidate", "scout marked as mission candidate")
		}
		if assessment.Class == "discard" {
			queue.SetStatus(cap.ID, "discarded", "scout: "+assessment.Reason)
			addTransition(cap, "discarded", "scout discarded capture before filing")
			_ = appendOfficerHandoff(dataDir, OfficerHandoffRecord{
				CaptureID:   cap.ID,
				Fingerprint: cap.Fingerprint,
				Source:      cap.Source,
				URL:         cap.URL,
				Title:       cap.Title,
				Scout:       assessment,
			})
			log.Printf("wraith/consumer: scout discarded %s — %s", cap.ID, truncate(cap.Title, 50))
			continue
		}

		// Build frontmatter
		fm := buildCaptureFrontmatter(cap)
		fm["scout_class"] = assessment.Class
		fm["scout_reason"] = assessment.Reason
		fm["scout_officer"] = assessment.Officer
		if assessment.Model != "" {
			fm["scout_model"] = assessment.Model
		}

		// Build body
		body := buildCaptureBody(cap)

		receipt, err := officers.Librarian.File(vaultDir, cap, fm, body)
		if err != nil {
			queue.SetStatus(cap.ID, "failed", err.Error())
			log.Printf("wraith/consumer: write error for %s: %v", cap.ID, err)
			continue
		}

		// Record in wraith state for dedup
		state.Record(source, externalID, cap.URL, cap.Title, receipt.VaultPath)

		// Update queue
		queue.SetVaultPath(cap.ID, receipt.VaultPath)
		queue.SetStatus(cap.ID, "done", "")
		_ = appendOfficerHandoff(dataDir, OfficerHandoffRecord{
			CaptureID:   cap.ID,
			Fingerprint: cap.Fingerprint,
			Source:      cap.Source,
			URL:         cap.URL,
			Title:       cap.Title,
			Scout:       assessment,
			Librarian:   &receipt,
		})

		count++
		log.Printf("wraith/consumer: wrote [%d] %s → %s", count, truncate(cap.Title, 50), filepath.Base(receipt.VaultPath))
	}

	log.Printf("wraith/consumer: processed %d captures, %d written", len(pending), count)
	return count, nil
}

func processDirectYouTubeCapture(queue *Queue, state *State, vaultDir string, cap *Capture) (bool, error) {
	if cap == nil {
		return false, nil
	}
	if isYouTubeWatchURL(cap.URL) {
		path, err := ingestYouTubeVideoFn(vaultDir, state, cap.URL)
		if err != nil {
			if strings.Contains(err.Error(), "already ingested") {
				queue.SetStatus(cap.ID, "deduped", err.Error())
				log.Printf("wraith/consumer: youtube duplicate %s — %s", cap.ID, truncate(cap.Title, 50))
				return true, nil
			}
			return true, err
		}
		queue.SetVaultPath(cap.ID, path)
		queue.SetStatus(cap.ID, "done", "youtube video routed")
		log.Printf("wraith/consumer: youtube video routed %s → %s", cap.ID, filepath.Base(path))
		return true, nil
	}
	if playlistID := extractYouTubePlaylistID(cap.URL); playlistID != "" {
		n, err := ingestYouTubeFn(vaultDir, state, []string{playlistID})
		if err != nil {
			return true, err
		}
		queue.SetStatus(cap.ID, "done", fmt.Sprintf("youtube playlist routed: %d new videos", n))
		queue.SetVaultPath(cap.ID, filepath.Join(vaultDir, "brain", "youtube"))
		log.Printf("wraith/consumer: youtube playlist routed %s — %d new videos", cap.ID, n)
		return true, nil
	}
	return false, nil
}

var youtubeShortIDPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]{11}$`)

func isYouTubeWatchURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "youtu.be":
		return youtubeShortIDPattern.MatchString(u.Path)
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		return u.Path == "/watch" && u.Query().Get("v") != ""
	default:
		return false
	}
}

func extractYouTubePlaylistID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" && host != "m.youtube.com" {
		return ""
	}
	listID := strings.TrimSpace(u.Query().Get("list"))
	if listID == "" {
		return ""
	}
	if u.Path == "/playlist" || u.Path == "/watch" {
		return listID
	}
	return ""
}

func captureSource(cap *Capture) string {
	if cap.Tweet != nil {
		return "x-extension"
	}
	if strings.Contains(cap.URL, "reddit.com") {
		return "reddit-extension"
	}
	return "extension-" + cap.Source
}

func captureExternalID(cap *Capture) string {
	if cap.Tweet != nil && cap.Tweet.TweetID != "" {
		return cap.Tweet.TweetID
	}
	// Use URL as external ID for general captures
	return cap.URL
}

func captureDir(vaultDir string, cap *Capture) string {
	if cap.Tweet != nil {
		return filepath.Join(vaultDir, "brain", "x")
	}
	if strings.Contains(cap.URL, "reddit.com") {
		return filepath.Join(vaultDir, "brain", "reddit")
	}
	return filepath.Join(vaultDir, "brain", "captures")
}

func buildCaptureFrontmatter(cap *Capture) map[string]interface{} {
	fm := map[string]interface{}{
		"source":      cap.Source,
		"url":         cap.URL,
		"date":        time.Now().Format("2006-01-02"),
		"triage":      "pending",
		"captured_at": cap.CapturedAt,
		"capture_id":  cap.ID,
	}

	if cap.Author != "" {
		fm["author"] = cap.Author
	}
	if cap.SiteName != "" {
		fm["site_name"] = cap.SiteName
	}

	if cap.Tweet != nil {
		fm["source"] = "x-extension"
		if cap.Tweet.Handle != "" {
			fm["author"] = cap.Tweet.Handle
		}
		if cap.Tweet.TweetID != "" {
			fm["tweet_id"] = cap.Tweet.TweetID
			fm["url"] = fmt.Sprintf("https://x.com/i/status/%s", cap.Tweet.TweetID)
		}
		if cap.Tweet.Likes != "" {
			fm["likes"] = cap.Tweet.Likes
		}
		if cap.Tweet.Retweets != "" {
			fm["retweets"] = cap.Tweet.Retweets
		}
		if len(cap.Tweet.MediaURLs) > 0 {
			fm["media"] = cap.Tweet.MediaURLs
		}
	}

	if cap.Selected != "" {
		fm["has_selection"] = true
	}

	return fm
}

func buildCaptureBody(cap *Capture) string {
	var sb strings.Builder

	// Title
	if cap.Title != "" {
		sb.WriteString("# ")
		sb.WriteString(cap.Title)
		sb.WriteString("\n\n")
	}

	// Tweet-specific body
	if cap.Tweet != nil {
		sb.WriteString(fmt.Sprintf("**%s** (%s)\n\n", cap.Tweet.Author, cap.Tweet.Handle))
		sb.WriteString(cap.Tweet.Text)
		sb.WriteString("\n")

		if cap.Tweet.QuotedText != "" {
			sb.WriteString(fmt.Sprintf("\n> **Quoted** (%s): %s\n", cap.Tweet.QuotedAuthor, cap.Tweet.QuotedText))
		}

		if len(cap.Tweet.ThreadTexts) > 0 {
			sb.WriteString("\n## Thread\n\n")
			for _, t := range cap.Tweet.ThreadTexts {
				sb.WriteString("- ")
				sb.WriteString(t)
				sb.WriteString("\n")
			}
		}

		if len(cap.Tweet.MediaURLs) > 0 {
			sb.WriteString("\n## Media\n\n")
			for _, u := range cap.Tweet.MediaURLs {
				sb.WriteString(fmt.Sprintf("![media](%s)\n", u))
			}
		}

		return sb.String()
	}

	// Selection if present
	if cap.Selected != "" {
		sb.WriteString("## Selected Text\n\n")
		sb.WriteString(cap.Selected)
		sb.WriteString("\n\n")
	}

	// Body text (truncated for vault files)
	if cap.BodyText != "" {
		body := cap.BodyText
		if len(body) > 5000 {
			body = body[:5000] + "\n\n[truncated]"
		}
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	// Links section
	if len(cap.Links) > 0 {
		sb.WriteString("\n## Links\n\n")
		limit := 20
		if len(cap.Links) < limit {
			limit = len(cap.Links)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString(fmt.Sprintf("- [%s](%s)\n", cap.Links[i].Text, cap.Links[i].Href))
		}
	}

	return sb.String()
}
