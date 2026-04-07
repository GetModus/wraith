package wraith

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/GetModus/wraith/internal/markdown"
)

const ytdlpBin = "/opt/homebrew/bin/yt-dlp"

func youtubeVaultDir(vaultDir string) string {
	return filepath.Join(vaultDir, "brain", "youtube")
}

// ytVideo holds metadata extracted from yt-dlp --dump-json output.
type ytVideo struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Channel     string      `json:"channel"`
	Uploader    string      `json:"uploader"`
	UploadDate  string      `json:"upload_date"` // YYYYMMDD
	Duration    float64     `json:"duration"`
	ViewCount   int         `json:"view_count"`
	LikeCount   int         `json:"like_count"`
	Description string      `json:"description"`
	Chapters    []ytChapter `json:"chapters"`
	WebpageURL  string      `json:"webpage_url"`
}

type ytChapter struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Title     string  `json:"title"`
}

// IngestYouTube fetches YouTube playlist items via yt-dlp (full metadata + transcripts).
// Falls back to RSS if yt-dlp is not available.
func IngestYouTube(vaultDir string, state *State, playlistIDs []string) (int, error) {
	if len(playlistIDs) == 0 {
		return 0, nil
	}

	dir := youtubeVaultDir(vaultDir)
	os.MkdirAll(dir, 0755)

	// Check if yt-dlp is available
	if _, err := exec.LookPath(ytdlpBin); err != nil {
		log.Println("wraith: yt-dlp not found, falling back to RSS")
		return ingestYouTubeRSS(vaultDir, state, playlistIDs)
	}

	total := 0

	for _, plID := range playlistIDs {
		playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", plID)
		log.Printf("wraith: enumerating playlist %s via yt-dlp...", plID)

		// Enumerate playlist entries (flat, fast)
		videoIDs, err := ytdlpEnumeratePlaylist(playlistURL)
		if err != nil {
			log.Printf("wraith: yt-dlp playlist enumerate failed for %s: %v — trying RSS fallback", plID, err)
			n, _ := ingestYouTubeRSS(vaultDir, state, []string{plID})
			total += n
			continue
		}

		log.Printf("wraith: playlist %s has %d videos", plID, len(videoIDs))

		for _, vid := range videoIDs {
			if state.Exists("youtube", vid) {
				continue
			}

			meta, transcript, err := ytdlpFetchVideo(vid)
			if err != nil {
				log.Printf("wraith: yt-dlp video %s failed: %v", vid, err)
				continue
			}

			// Format date
			date := time.Now().Format("2006-01-02")
			if len(meta.UploadDate) == 8 {
				date = meta.UploadDate[:4] + "-" + meta.UploadDate[4:6] + "-" + meta.UploadDate[6:8]
			}

			slug := slugify(meta.Title)
			if slug == "" {
				slug = "yt-" + vid
			}
			filename := fmt.Sprintf("%s-%s.md", date, slug)
			path := filepath.Join(dir, filename)

			url := "https://www.youtube.com/watch?v=" + vid
			channel := meta.Channel
			if channel == "" {
				channel = meta.Uploader
			}

			var extracted string
			if transcript != "" {
				log.Printf("wraith: running librarian extraction for playlist video %s (%d chars transcript)...", vid, len(transcript))
				extracted = librarianExtract(meta.Title, transcript)
				if extracted != "" {
					log.Printf("wraith: playlist extraction complete for %s: %d chars", vid, len(extracted))
				} else {
					log.Printf("wraith: playlist extraction failed for %s — saving raw transcript", vid)
				}
			}

			// Build body
			var body strings.Builder
			fmt.Fprintf(&body, "# %s\n\n", meta.Title)
			fmt.Fprintf(&body, "Channel: %s\n", channel)
			fmt.Fprintf(&body, "Published: %s\n", date)
			fmt.Fprintf(&body, "Duration: %s\n", formatDuration(meta.Duration))
			if meta.ViewCount > 0 {
				fmt.Fprintf(&body, "Views: %d\n", meta.ViewCount)
			}

			if meta.Description != "" {
				desc := meta.Description
				if len(desc) > 2000 {
					desc = desc[:2000] + "\n[... truncated]"
				}
				fmt.Fprintf(&body, "\n## Description\n\n%s\n", desc)
			}

			if extracted != "" {
				fmt.Fprintf(&body, "\n---\n\n## Librarian Extraction\n\n%s\n", extracted)
			}

			if len(meta.Chapters) > 0 {
				body.WriteString("\n## Chapters\n\n")
				for _, ch := range meta.Chapters {
					fmt.Fprintf(&body, "- [%s] %s\n", formatDuration(ch.StartTime), ch.Title)
				}
			}

			if transcript != "" {
				fmt.Fprintf(&body, "\n## Transcript\n\n%s\n", transcript)
			}

			fm := map[string]interface{}{
				"source":      "youtube",
				"playlist_id": plID,
				"video_id":    vid,
				"channel":     channel,
				"url":         url,
				"date":        date,
				"duration":    int(meta.Duration),
				"triage":      "pending",
			}
			if transcript != "" {
				fm["has_transcript"] = true
			}
			if extracted != "" {
				fm["has_extraction"] = true
			}
			if len(meta.Chapters) > 0 {
				fm["has_chapters"] = true
			}

			if err := markdown.Write(path, fm, body.String()); err != nil {
				log.Printf("wraith: write error for video %s: %v", vid, err)
				continue
			}

			if err := state.Record("youtube", vid, url, meta.Title, path); err != nil {
				log.Printf("wraith: state record error: %v", err)
			}

			total++
			transcriptTag := ""
			if transcript != "" {
				transcriptTag = " [+transcript]"
			}
			log.Printf("wraith: ingested youtube [%d] %s: %s%s", total, channel, truncate(meta.Title, 50), transcriptTag)
		}
	}

	log.Printf("wraith: YouTube — %d new videos from %d playlists", total, len(playlistIDs))
	return total, nil
}

// ytdlpEnumeratePlaylist returns video IDs in a playlist using yt-dlp --flat-playlist.
func ytdlpEnumeratePlaylist(playlistURL string) ([]string, error) {
	cmd := exec.Command(ytdlpBin,
		"--flat-playlist",
		"--dump-json",
		"--no-warnings",
		playlistURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp flat-playlist: %w", err)
	}

	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.ID != "" {
			ids = append(ids, entry.ID)
		}
	}

	return ids, nil
}

// ytdlpFetchVideo fetches full metadata and transcript for a single video.
// Two-step process:
// 1. yt-dlp --dump-json for metadata (always succeeds)
// 2. yt-dlp --write-subs for transcript (best-effort, may fail on rate limits)
func ytdlpFetchVideo(videoID string) (*ytVideo, string, error) {
	vidURL := "https://www.youtube.com/watch?v=" + videoID

	// Step 1: metadata only (fast, reliable)
	cmd := exec.Command(ytdlpBin,
		"--skip-download",
		"--dump-json",
		"--no-warnings",
		vidURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("yt-dlp: %w", err)
	}

	var meta ytVideo
	if err := json.Unmarshal(out, &meta); err != nil {
		return nil, "", fmt.Errorf("yt-dlp JSON parse: %w", err)
	}

	// Step 2: subtitle download (best-effort, separate call)
	transcript := ytdlpFetchTranscript(videoID)

	return &meta, transcript, nil
}

// ytdlpFetchTranscript uses yt-dlp to download subtitles for a video.
// Returns empty string on failure (rate limits, no subs available, etc).
func ytdlpFetchTranscript(videoID string) string {
	tmpDir, err := os.MkdirTemp("", "wraith-yt-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmpDir)

	vidURL := "https://www.youtube.com/watch?v=" + videoID

	cmd := exec.Command(ytdlpBin,
		"--skip-download",
		"--write-subs",
		"--write-auto-subs",
		"--sub-langs", "en",
		"--sub-format", "json3",
		"--no-warnings",
		"-o", filepath.Join(tmpDir, "%(id)s.%(ext)s"),
		vidURL,
	)
	// Ignore exit code — subtitle download may fail (429, no subs, etc)
	cmd.Run()

	// Look for any json3 file
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.json3"))
	for _, path := range matches {
		if data, err := os.ReadFile(path); err == nil {
			if t := parseJSON3Transcript(data); t != "" {
				return t
			}
		}
	}

	return ""
}

// parseJSON3Transcript extracts plain text from YouTube json3 subtitle format.
func parseJSON3Transcript(data []byte) string {
	var sub struct {
		Events []struct {
			TStartMs int64 `json:"tStartMs"`
			Segs     []struct {
				UTF8 string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}

	if err := json.Unmarshal(data, &sub); err != nil {
		return ""
	}

	var lines []string
	var current strings.Builder
	lastTimestamp := int64(-1)

	for _, event := range sub.Events {
		if len(event.Segs) == 0 {
			continue
		}

		// Group into ~30s chunks with timestamps
		if lastTimestamp < 0 || event.TStartMs-lastTimestamp > 30000 {
			if current.Len() > 0 {
				lines = append(lines, current.String())
				current.Reset()
			}
			ts := formatDuration(float64(event.TStartMs) / 1000)
			current.WriteString("[" + ts + "] ")
			lastTimestamp = event.TStartMs
		}

		for _, seg := range event.Segs {
			text := strings.TrimSpace(seg.UTF8)
			if text != "" && text != "\n" {
				current.WriteString(text + " ")
			}
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}

	result := strings.Join(lines, "\n")

	// Cap at 50K characters
	if len(result) > 50000 {
		result = result[:50000] + "\n[... transcript truncated]"
	}

	return strings.TrimSpace(result)
}

// formatDuration converts seconds to HH:MM:SS or MM:SS.
func formatDuration(seconds float64) string {
	s := int(seconds)
	if s < 0 {
		s = 0
	}
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// ─────────────────────────────────────────────────────────
// Single-video ingestion with librarian extraction
// ─────────────────────────────────────────────────────────

// extractVideoID parses a YouTube video ID from various URL formats.
func extractVideoID(url string) string {
	patterns := []string{
		`(?:v=|/v/|youtu\.be/)([a-zA-Z0-9_-]{11})`,
		`(?:embed/)([a-zA-Z0-9_-]{11})`,
		`^([a-zA-Z0-9_-]{11})$`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(url)
		if len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// IngestYouTubeVideo fetches a single YouTube video, extracts its transcript,
// runs it through the local librarian for structured extraction, and writes
// both raw and extracted versions to the vault.
func IngestYouTubeVideo(vaultDir string, state *State, videoURL string) (string, error) {
	vid := extractVideoID(videoURL)
	if vid == "" {
		return "", fmt.Errorf("could not extract video ID from: %s", videoURL)
	}

	// Check dedup
	if state != nil && state.Exists("youtube", vid) {
		return "", fmt.Errorf("video %s already ingested", vid)
	}

	log.Printf("wraith: fetching video %s...", vid)

	meta, transcript, err := ytdlpFetchVideo(vid)
	if err != nil {
		return "", fmt.Errorf("fetch video: %w", err)
	}

	if transcript == "" {
		log.Printf("wraith: no transcript for %s — saving metadata only", vid)
	}

	// Format date
	date := time.Now().Format("2006-01-02")
	if len(meta.UploadDate) == 8 {
		date = meta.UploadDate[:4] + "-" + meta.UploadDate[4:6] + "-" + meta.UploadDate[6:8]
	}

	channel := meta.Channel
	if channel == "" {
		channel = meta.Uploader
	}

	dir := youtubeVaultDir(vaultDir)
	os.MkdirAll(dir, 0755)

	slug := slugify(meta.Title)
	if slug == "" {
		slug = "yt-" + vid
	}

	// Run librarian extraction if transcript is available
	var extracted string
	if transcript != "" {
		log.Printf("wraith: running librarian extraction for %s (%d chars transcript)...", vid, len(transcript))
		extracted = librarianExtract(meta.Title, transcript)
		if extracted != "" {
			log.Printf("wraith: extraction complete: %d chars", len(extracted))
		} else {
			log.Printf("wraith: extraction failed — saving raw transcript")
		}
	}

	// Write FULL content: metadata + extraction + chapters + raw transcript
	filename := fmt.Sprintf("%s-%s.md", vid, slug)
	path := filepath.Join(dir, filename)

	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", meta.Title)
	fmt.Fprintf(&body, "Channel: %s\n", channel)
	fmt.Fprintf(&body, "Published: %s\n", date)
	fmt.Fprintf(&body, "Duration: %s\n", formatDuration(meta.Duration))
	if meta.ViewCount > 0 {
		fmt.Fprintf(&body, "Views: %d\n", meta.ViewCount)
	}

	// Librarian extraction (structured knowledge)
	if extracted != "" {
		fmt.Fprintf(&body, "\n---\n\n## Librarian Extraction\n\n%s\n", extracted)
	}

	// Description
	if meta.Description != "" {
		desc := meta.Description
		if len(desc) > 4000 {
			desc = desc[:4000] + "\n[... truncated]"
		}
		fmt.Fprintf(&body, "\n---\n\n## Description\n\n%s\n", desc)
	}

	// Chapters
	if len(meta.Chapters) > 0 {
		body.WriteString("\n## Chapters\n\n")
		for _, ch := range meta.Chapters {
			fmt.Fprintf(&body, "- [%s] %s\n", formatDuration(ch.StartTime), ch.Title)
		}
	}

	// Full transcript (always include)
	if transcript != "" {
		fmt.Fprintf(&body, "\n---\n\n## Transcript\n\n%s\n", transcript)
	}

	url := "https://www.youtube.com/watch?v=" + vid
	tags := []string{"youtube"}
	if extracted != "" {
		tags = append(tags, "librarian-extracted")
	}

	fm := map[string]interface{}{
		"title":    meta.Title,
		"source":   url,
		"type":     "youtube",
		"video_id": vid,
		"channel":  channel,
		"date":     date,
		"duration": formatDuration(meta.Duration),
		"tags":     tags,
	}
	if transcript != "" {
		fm["has_transcript"] = true
	}
	if extracted != "" {
		fm["has_extraction"] = true
	}

	if err := markdown.Write(path, fm, body.String()); err != nil {
		return "", fmt.Errorf("write vault: %w", err)
	}

	if state != nil {
		state.Record("youtube", vid, url, meta.Title, path)
	}

	log.Printf("wraith: ingested %s → %s", meta.Title, path)
	return path, nil
}

// librarianExtract sends a transcript to the Gemma 4 26B MoE librarian
// (llama-server, OpenAI-compatible) for structured knowledge extraction.
// Returns empty string on failure.
func librarianExtract(title, transcript string) string {
	// Librarian runs on llama-server port 8090 (Gemma 4 26B-A4B MoE)
	librarianURL := os.Getenv("MODUS_LIBRARIAN_URL")
	if librarianURL == "" {
		librarianURL = "http://127.0.0.1:8090/v1"
	}

	system := `You are extracting structured knowledge from a YouTube video transcript.
Output the following sections. Be thorough and detailed. Skip sections that don't apply.

## Summary
1-3 sentence overview of the video.

## Key Ideas
- Bullet each distinct idea or insight (aim for 10-20)
- Include supporting detail and context for each point

## Technical Details
- Specific tools, libraries, methods, architectures, benchmarks mentioned
- Version numbers, performance figures, comparisons
- Code patterns, API designs, or implementation strategies discussed

## Actionable Takeaways
- What a developer building AI infrastructure should DO based on this
- Be specific: "try X", "replace Y with Z", "benchmark A vs B"

## Quotes
- Notable direct quotes (max 10), attributed if speaker is named

## References
- Papers, repos, tools, or resources mentioned (with URLs if stated)`

	// Cap transcript at 30K chars
	if len(transcript) > 30000 {
		transcript = transcript[:30000] + "\n\n[transcript truncated]"
	}

	prompt := fmt.Sprintf("Video: %s\n\nTranscript:\n%s", title, transcript)

	reqBody := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  4096,
		"temperature": 0.2,
		"chat_template_kwargs": map[string]interface{}{
			"enable_thinking": false,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("wraith: librarian marshal error: %v", err)
		return ""
	}

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Post(librarianURL+"/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("wraith: librarian request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("wraith: librarian read error: %v", err)
		return ""
	}

	if resp.StatusCode != 200 {
		log.Printf("wraith: librarian error %d: %s", resp.StatusCode, string(respBody[:min(200, len(respBody))]))
		return ""
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		log.Printf("wraith: librarian parse error: %v", err)
		return ""
	}

	if len(chatResp.Choices) == 0 {
		log.Printf("wraith: librarian returned no choices")
		return ""
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if len(content) < 50 {
		log.Printf("wraith: librarian returned insufficient output (%d chars)", len(content))
		return ""
	}

	log.Printf("wraith: librarian: %d completion tokens, %d chars", chatResp.Usage.CompletionTokens, len(content))

	return content
}

// -------- RSS fallback (original implementation) --------

// ingestYouTubeRSS is the fallback when yt-dlp is not available.
func ingestYouTubeRSS(vaultDir string, state *State, playlistIDs []string) (int, error) {
	dir := filepath.Join(vaultDir, "brain", "youtube")
	os.MkdirAll(dir, 0755)

	client := &http.Client{Timeout: 15 * time.Second}
	total := 0

	for _, plID := range playlistIDs {
		feedURL := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?playlist_id=%s", plID)

		resp, err := client.Get(feedURL)
		if err != nil {
			log.Printf("wraith: youtube RSS error for %s: %v", plID, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("wraith: youtube RSS %d for playlist %s", resp.StatusCode, plID)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		entries := parseYouTubeRSS(string(body))
		count := 0

		for _, entry := range entries {
			if entry.VideoID == "" {
				continue
			}

			if state.Exists("youtube", entry.VideoID) {
				continue
			}

			date := time.Now().Format("2006-01-02")
			if entry.Published != "" && len(entry.Published) >= 10 {
				date = entry.Published[:10]
			}

			slug := slugify(entry.Title)
			if slug == "" {
				slug = "yt-" + entry.VideoID
			}
			filename := fmt.Sprintf("%s-%s.md", date, slug)
			path := filepath.Join(dir, filename)

			url := "https://www.youtube.com/watch?v=" + entry.VideoID

			mdBody := fmt.Sprintf("# %s\n\nChannel: %s\nPublished: %s\n\n%s",
				entry.Title, entry.Author, entry.Published, entry.Description)

			fm := map[string]interface{}{
				"source":      "youtube",
				"playlist_id": plID,
				"video_id":    entry.VideoID,
				"channel":     entry.Author,
				"url":         url,
				"date":        date,
				"triage":      "pending",
			}

			if err := markdown.Write(path, fm, mdBody); err != nil {
				log.Printf("wraith: write error for video %s: %v", entry.VideoID, err)
				continue
			}

			if err := state.Record("youtube", entry.VideoID, url, entry.Title, path); err != nil {
				log.Printf("wraith: state record error: %v", err)
			}

			count++
			total++
			log.Printf("wraith: ingested youtube [%d] %s: %s", count, entry.Author, truncate(entry.Title, 60))
		}
	}

	log.Printf("wraith: YouTube (RSS fallback) — %d new videos from %d playlists", total, len(playlistIDs))
	return total, nil
}

// ytEntry represents a parsed YouTube RSS entry.
type ytEntry struct {
	VideoID     string
	Title       string
	Author      string
	Published   string
	Description string
}

func parseYouTubeRSS(xml string) []ytEntry {
	var entries []ytEntry

	entryRegex := regexp.MustCompile(`(?s)<entry>(.*?)</entry>`)
	videoIDRegex := regexp.MustCompile(`<yt:videoId>(.*?)</yt:videoId>`)
	titleRegex := regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	authorRegex := regexp.MustCompile(`(?s)<author>.*?<name>(.*?)</name>.*?</author>`)
	publishedRegex := regexp.MustCompile(`<published>(.*?)</published>`)
	descRegex := regexp.MustCompile(`(?s)<media:description>(.*?)</media:description>`)

	matches := entryRegex.FindAllStringSubmatch(xml, -1)
	for _, match := range matches {
		content := match[1]
		entry := ytEntry{}

		if m := videoIDRegex.FindStringSubmatch(content); len(m) > 1 {
			entry.VideoID = strings.TrimSpace(m[1])
		}
		if m := titleRegex.FindStringSubmatch(content); len(m) > 1 {
			entry.Title = strings.TrimSpace(m[1])
		}
		if m := authorRegex.FindStringSubmatch(content); len(m) > 1 {
			entry.Author = strings.TrimSpace(m[1])
		}
		if m := publishedRegex.FindStringSubmatch(content); len(m) > 1 {
			entry.Published = strings.TrimSpace(m[1])
		}
		if m := descRegex.FindStringSubmatch(content); len(m) > 1 {
			desc := strings.TrimSpace(m[1])
			if len(desc) > 2000 {
				desc = desc[:2000] + "..."
			}
			entry.Description = desc
		}

		if entry.VideoID != "" {
			entries = append(entries, entry)
		}
	}

	return entries
}
