package wraith

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Capture represents a raw artifact from the Safari extension.
type Capture struct {
	ID         string                 `json:"id"`
	Source     string                 `json:"source"` // "extension", "context-menu", "bookmark-sync"
	URL        string                 `json:"url"`
	Title      string                 `json:"title"`
	SiteName   string                 `json:"site_name,omitempty"`
	Author     string                 `json:"author,omitempty"`
	BodyText   string                 `json:"body_text,omitempty"`
	Selected   string                 `json:"selected,omitempty"`
	Headings   []CaptureHeading       `json:"headings,omitempty"`
	Links      []CaptureLink          `json:"links,omitempty"`
	Images     []CaptureImage         `json:"images,omitempty"`
	Tweet      *CaptureTweet          `json:"tweet,omitempty"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
	CapturedAt string                 `json:"captured_at"`
	Status     string                 `json:"status"` // "queued", "processing", "done", "failed", "deduped"
	// IngestState is the canonical ledger state used by the ingest audit trail.
	// Allowed values: captured, queued, deduped, processing, triaged, filed, discarded, mission_candidate, failed.
	IngestState string `json:"ingest_state,omitempty"`
	// IngestHistory records state transitions for this capture.
	IngestHistory []IngestTransition `json:"ingest_history,omitempty"`
	// Fingerprint is a deterministic dedup key for queue ingress.
	Fingerprint string `json:"fingerprint,omitempty"`
	// DuplicateOf points to the canonical capture ID when this item is deduped at ingress.
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// DuplicateCount tracks how many ingress duplicates have folded into this capture.
	DuplicateCount int    `json:"duplicate_count,omitempty"`
	LastSeenAt     string `json:"last_seen_at,omitempty"`
	Error          string `json:"error,omitempty"`
	VaultPath      string `json:"vault_path,omitempty"`
}

// IngestTransition records a state transition in the capture ingest ledger.
type IngestTransition struct {
	State string `json:"state"`
	At    string `json:"at"`
	Note  string `json:"note,omitempty"`
}

// CaptureHeading is a heading extracted from a page.
type CaptureHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// CaptureLink is a link extracted from a page.
type CaptureLink struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

// CaptureImage is an image extracted from a page.
type CaptureImage struct {
	Src    string `json:"src"`
	Alt    string `json:"alt"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// CaptureTweet holds X/Twitter-specific extracted data.
type CaptureTweet struct {
	TweetID      string   `json:"tweet_id,omitempty"`
	Author       string   `json:"author"`
	Handle       string   `json:"handle"`
	Text         string   `json:"text"`
	Timestamp    string   `json:"timestamp,omitempty"`
	Likes        string   `json:"likes,omitempty"`
	Retweets     string   `json:"retweets,omitempty"`
	Replies      string   `json:"replies,omitempty"`
	QuotedText   string   `json:"quoted_text,omitempty"`
	QuotedAuthor string   `json:"quoted_author,omitempty"`
	MediaURLs    []string `json:"media_urls,omitempty"`
	ThreadTexts  []string `json:"thread_texts,omitempty"`
}

// Queue is a durable capture queue backed by a JSON file.
type Queue struct {
	mu       sync.RWMutex
	path     string
	captures []*Capture
}

// OpenQueue opens or creates the capture queue file.
func OpenQueue(dataDir string) (*Queue, error) {
	os.MkdirAll(dataDir, 0755)
	path := filepath.Join(dataDir, "wraith-queue.json")

	q := &Queue{
		path:     path,
		captures: make([]*Capture, 0),
	}

	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &q.captures); err != nil {
			log.Printf("wraith/queue: parse error (starting fresh): %v", err)
			q.captures = make([]*Capture, 0)
		}
	}

	return q, nil
}

// Enqueue adds a new capture to the queue and persists.
func (q *Queue) Enqueue(c *Capture) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	if c.ID == "" {
		c.ID = generateID()
	}
	if c.CapturedAt == "" {
		c.CapturedAt = now
	}
	c.LastSeenAt = now
	if c.Fingerprint == "" {
		c.Fingerprint = buildFingerprint(c)
	}

	// First-pass queue ingress dedup. Keep a record for audit, but do not reprocess.
	if existing := q.findByFingerprint(c.Fingerprint); existing != nil {
		existing.DuplicateCount++
		existing.LastSeenAt = now
		addTransition(existing, "deduped", fmt.Sprintf("ingress duplicate capture %s folded into %s", c.ID, existing.ID))

		c.Status = "deduped"
		c.IngestState = "deduped"
		c.DuplicateOf = existing.ID
		addTransition(c, "captured", "received by bridge")
		addTransition(c, "deduped", fmt.Sprintf("duplicate of %s", existing.ID))

		q.captures = append(q.captures, c)
		log.Printf("wraith/queue: deduped ingress %s -> %s (%s)", c.ID, existing.ID, truncate(c.Title, 60))
		return q.save()
	}
	// Semantic-ish near-duplicate fold for captures that share normalized URL and
	// highly similar extracted text but differ in minor runtime noise.
	if existing, sim := q.findNearDuplicate(c); existing != nil {
		existing.DuplicateCount++
		existing.LastSeenAt = now
		addTransition(existing, "deduped", fmt.Sprintf("near-duplicate capture %s folded into %s (sim=%.2f)", c.ID, existing.ID, sim))

		c.Status = "deduped"
		c.IngestState = "deduped"
		c.DuplicateOf = existing.ID
		if c.Meta == nil {
			c.Meta = map[string]interface{}{}
		}
		c.Meta["near_duplicate_similarity"] = sim
		addTransition(c, "captured", "received by bridge")
		addTransition(c, "deduped", fmt.Sprintf("near-duplicate of %s (sim=%.2f)", existing.ID, sim))

		q.captures = append(q.captures, c)
		log.Printf("wraith/queue: near-deduped ingress %s -> %s (sim=%.2f, %s)", c.ID, existing.ID, sim, truncate(c.Title, 60))
		return q.save()
	}

	if c.Status == "" {
		c.Status = "queued"
	}
	if c.IngestState == "" {
		c.IngestState = "captured"
		addTransition(c, "captured", "received by bridge")
	}
	if c.Status == "queued" {
		addTransition(c, "queued", "accepted into queue")
	}

	q.captures = append(q.captures, c)
	log.Printf("wraith/queue: enqueued %s — %s (%s)", c.ID, truncate(c.Title, 60), c.Source)
	return q.save()
}

// Pending returns captures that haven't been processed yet.
func (q *Queue) Pending(limit int) []*Capture {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var pending []*Capture
	for _, c := range q.captures {
		if c.Status == "queued" {
			pending = append(pending, c)
		}
	}

	// Oldest first
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CapturedAt < pending[j].CapturedAt
	})

	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending
}

// SetStatus updates a capture's status and persists.
func (q *Queue) SetStatus(id, status, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, c := range q.captures {
		if c.ID == id {
			c.Status = status
			c.Error = errMsg
			switch status {
			case "queued":
				c.IngestState = "queued"
				addTransition(c, "queued", "queued for processing")
			case "processing":
				c.IngestState = "processing"
				addTransition(c, "processing", "consumer started processing")
			case "done":
				c.IngestState = "filed"
				addTransition(c, "filed", "capture persisted to vault")
			case "failed":
				c.IngestState = "failed"
				addTransition(c, "failed", errMsg)
			case "deduped":
				c.IngestState = "deduped"
				addTransition(c, "deduped", errMsg)
			default:
				addTransition(c, status, errMsg)
			}
			return q.save()
		}
	}
	return fmt.Errorf("capture %s not found", id)
}

// SetVaultPath records where the capture was written in the vault.
func (q *Queue) SetVaultPath(id, path string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, c := range q.captures {
		if c.ID == id {
			c.VaultPath = path
			if path != "" {
				addTransition(c, "filed", fmt.Sprintf("written to %s", path))
			}
			return q.save()
		}
	}
	return fmt.Errorf("capture %s not found", id)
}

// Stats returns queue statistics.
func (q *Queue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var s QueueStats
	s.Total = len(q.captures)
	for _, c := range q.captures {
		switch c.Status {
		case "queued":
			s.Queued++
		case "processing":
			s.Processing++
		case "done":
			s.Done++
		case "failed":
			s.Failed++
		case "deduped":
			s.Deduped++
		case "discarded":
			s.Discarded++
		}
		if c.CapturedAt > s.LastCapture {
			s.LastCapture = c.CapturedAt
		}
	}
	return s
}

// Prune removes completed captures older than the given duration.
func (q *Queue) Prune(olderThan time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339)
	var kept []*Capture
	pruned := 0
	for _, c := range q.captures {
		if (c.Status == "done" || c.Status == "failed" || c.Status == "deduped" || c.Status == "discarded") && c.CapturedAt < cutoff {
			pruned++
		} else {
			kept = append(kept, c)
		}
	}
	q.captures = kept
	if pruned > 0 {
		q.save()
	}
	return pruned
}

// QueueStats holds queue statistics.
type QueueStats struct {
	Total       int    `json:"total"`
	Queued      int    `json:"queued"`
	Processing  int    `json:"processing"`
	Done        int    `json:"done"`
	Failed      int    `json:"failed"`
	Deduped     int    `json:"deduped"`
	Discarded   int    `json:"discarded"`
	LastCapture string `json:"last_capture,omitempty"`
}

func (q *Queue) save() error {
	data, err := json.MarshalIndent(q.captures, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal queue: %w", err)
	}
	return os.WriteFile(q.path, data, 0644)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (q *Queue) findByFingerprint(fp string) *Capture {
	if strings.TrimSpace(fp) == "" {
		return nil
	}
	for _, c := range q.captures {
		if c.Fingerprint == fp && c.Status != "failed" {
			return c
		}
	}
	return nil
}

func (q *Queue) findNearDuplicate(c *Capture) (*Capture, float64) {
	urlKey := normalizeURLKey(c.URL)
	if strings.TrimSpace(urlKey) == "" {
		return nil, 0
	}
	bestSim := 0.0
	var best *Capture
	for _, existing := range q.captures {
		if existing == nil || existing.Status == "failed" {
			continue
		}
		if normalizeURLKey(existing.URL) != urlKey {
			continue
		}
		sim := captureSimilarity(existing, c)
		if sim >= 0.82 && sim > bestSim {
			bestSim = sim
			best = existing
		}
	}
	return best, bestSim
}

func addTransition(c *Capture, state, note string) {
	if c == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if len(c.IngestHistory) > 0 {
		last := c.IngestHistory[len(c.IngestHistory)-1]
		if last.State == state {
			return
		}
	}
	c.IngestHistory = append(c.IngestHistory, IngestTransition{
		State: state,
		At:    now,
		Note:  strings.TrimSpace(note),
	})
	c.IngestState = state
}

func buildFingerprint(c *Capture) string {
	source := strings.TrimSpace(strings.ToLower(c.Source))
	url := normalizeURLKey(c.URL)
	tweetID := ""
	if c.Tweet != nil {
		tweetID = strings.TrimSpace(c.Tweet.TweetID)
	}
	title := strings.TrimSpace(strings.ToLower(c.Title))
	body := strings.TrimSpace(strings.ToLower(c.BodyText))
	if len(body) > 512 {
		body = body[:512]
	}
	raw := strings.Join([]string{source, tweetID, url, title, body}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:12])
}

func captureSimilarity(a, b *Capture) float64 {
	if a == nil || b == nil {
		return 0
	}
	at := strings.TrimSpace(strings.ToLower(a.Title))
	bt := strings.TrimSpace(strings.ToLower(b.Title))
	ab := strings.TrimSpace(strings.ToLower(a.BodyText))
	bb := strings.TrimSpace(strings.ToLower(b.BodyText))
	if len(ab) > 1024 {
		ab = ab[:1024]
	}
	if len(bb) > 1024 {
		bb = bb[:1024]
	}
	combinedA := strings.TrimSpace(at + " " + ab)
	combinedB := strings.TrimSpace(bt + " " + bb)
	return jaccardWordSimilarity(combinedA, combinedB)
}

func jaccardWordSimilarity(a, b string) float64 {
	setA := tokenizeForSimilarity(a)
	setB := tokenizeForSimilarity(b)
	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}
	if len(setA) == 0 || len(setB) == 0 {
		return 0.0
	}

	intersection := 0
	for token := range setA {
		if setB[token] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union <= 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

func tokenizeForSimilarity(text string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		tok = strings.Trim(tok, ".,;:!?\"'`()[]{}<>|_/\\-")
		if len(tok) < 3 {
			continue
		}
		out[tok] = true
	}
	return out
}

func normalizeURLKey(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	u = strings.TrimSuffix(u, "/")
	return u
}
