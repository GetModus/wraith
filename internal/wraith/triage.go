package wraith

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Librarian endpoint — Gemma 4 26B-A4B Q4_K_M on llama-server.
// Hardcoded. If down, triage degrades to raw FTS5 (no silent wrong-model).
const librarianEndpoint = "http://127.0.0.1:8090"

// maxChallengeRounds caps the dialectic loop per ADAPT item.
const maxChallengeRounds = 3

// RouteTriagedFile moves a vault file to the appropriate triage folder.
// ADAPT files stay in place (missions handle them). KEEP goes to brain/keep/.
// DISCARD goes to brain/discard/. MORE_INFO goes to brain/pending/.
func RouteTriagedFile(vaultDir string, r TriageResult) error {
	if r.VaultPath == "" {
		return nil
	}
	if _, err := os.Stat(r.VaultPath); os.IsNotExist(err) {
		return nil
	}

	var destDir string
	switch r.Class {
	case "KEEP":
		destDir = filepath.Join(vaultDir, "brain", "keep")
	case "DISCARD":
		destDir = filepath.Join(vaultDir, "brain", "discard")
	case "MORE_INFO":
		destDir = filepath.Join(vaultDir, "brain", "pending")
	default:
		// ADAPT stays in place — mission created separately
		return nil
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	base := filepath.Base(r.VaultPath)
	dest := filepath.Join(destDir, base)

	if err := os.Rename(r.VaultPath, dest); err != nil {
		return fmt.Errorf("move %s → %s: %w", r.VaultPath, dest, err)
	}

	log.Printf("wraith/triage: routed %s → %s", base, destDir)
	return nil
}

// TriageResult holds the classification for a single item.
type TriageResult struct {
	Source     string
	ExternalID string
	Title      string
	Class      string // ADAPT, KEEP, MORE_INFO, DISCARD
	Reason     string
	Challenged bool   // true if this ADAPT survived dialectic
	VaultPath  string // path to the .md file in vault/brain/
}

// Triage classifies pending items using the librarian (GLM on :8090).
// ADAPT items go through a dialectic challenge loop before final classification.
func Triage(state *State, vaultDir string, maxItems int) ([]TriageResult, error) {
	if maxItems <= 0 {
		maxItems = 20
	}

	pending, err := state.PendingTriage(maxItems)
	if err != nil {
		return nil, fmt.Errorf("fetch pending: %w", err)
	}

	if len(pending) == 0 {
		log.Println("wraith/triage: no pending items")
		return nil, nil
	}

	log.Printf("wraith/triage: classifying %d pending items", len(pending))

	if !librarianAvailable() {
		log.Println("wraith/triage: librarian unavailable on :8090 — skipping")
		return nil, fmt.Errorf("librarian not available at %s", librarianEndpoint)
	}

	// Phase 1: Batch classify all items
	items := make([]batchItem, 0, len(pending))
	for _, p := range pending {
		content := p.Title
		if p.VaultPath != "" {
			if data, err := os.ReadFile(p.VaultPath); err == nil {
				body := extractBody(string(data))
				if len(body) > 1000 {
					body = body[:1000]
				}
				content = body
			}
		}
		items = append(items, batchItem{
			ID:      p.ExternalID,
			Source:  p.Source,
			Title:   p.Title,
			Content: content,
		})
	}

	classifications := classifyBatch(items)

	// Phase 2: Dialectic challenge for ADAPT items
	var results []TriageResult
	for i, c := range classifications {
		p := pending[i]

		if c.Class == "ADAPT" {
			challenged := dialecticChallenge(p.Title, items[i].Content, c.Reason)
			c.Class = challenged.FinalClass
			c.Reason = challenged.FinalReason
			if challenged.Survived {
				log.Printf("wraith/triage: [ADAPT-CONFIRMED] %s — survived %d rounds",
					truncate(p.Title, 40), challenged.Rounds)
			} else {
				log.Printf("wraith/triage: [ADAPT→%s] %s — demoted after challenge",
					c.Class, truncate(p.Title, 40))
			}
		}

		if err := state.SetTriage(p.Source, p.ExternalID, c.Class); err != nil {
			log.Printf("wraith/triage: update error for %s: %v", p.ExternalID, err)
		}

		results = append(results, TriageResult{
			Source:     p.Source,
			ExternalID: p.ExternalID,
			Title:      p.Title,
			Class:      c.Class,
			Reason:     c.Reason,
			Challenged: c.Class == "ADAPT",
			VaultPath:  p.VaultPath,
		})

		log.Printf("wraith/triage: [%s] %s — %s", c.Class, truncate(p.Title, 50), truncate(c.Reason, 60))
	}

	return results, nil
}

// ── Batch Classification ─────────────────────────────────────────────

type batchItem struct {
	ID      string
	Source  string
	Title   string
	Content string
}

type batchClassification struct {
	ID     json.Number `json:"id"`
	Class  string      `json:"label"`
	Reason string      `json:"reason"`
}

// classifyBatch sends all items to the librarian in one call.
func classifyBatch(items []batchItem) []batchClassification {
	var sb strings.Builder
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n   %s\n\n",
			i+1, item.Source, item.Title, truncate(item.Content, 300)))
	}

	system := `You classify content for a developer building AI infrastructure (local models, llama.cpp, Apple Silicon, Go, memory systems, security monitoring, revenue generation).

Classify each item:
- ADAPT: Directly actionable — tools, techniques, code, benchmarks we should implement
- KEEP: Useful reference — industry news, specs, announcements relevant to our domain
- MORE_INFO: Potentially useful but title/snippet insufficient to judge — need full article
- DISCARD: Irrelevant — entertainment, sales, politics, gaming, off-topic

Return JSON array: [{"id": 1, "label": "X", "reason": "one line why"}]
Every item must appear. No extra text.`

	user := fmt.Sprintf("Classify ALL %d items:\n%s", len(items), sb.String())

	content := callLibrarian(system, user, 1200)

	var results []batchClassification
	cleaned := stripFences(content)
	if err := json.Unmarshal([]byte(cleaned), &results); err != nil {
		log.Printf("wraith/triage: batch parse failed, falling back to per-item: %v", err)
		return classifyFallback(items)
	}

	// Map results back by 1-indexed ID from prompt
	ordered := make([]batchClassification, len(items))
	for i := range items {
		found := false
		for _, r := range results {
			idNum, _ := r.ID.Int64()
			if int(idNum) == i+1 {
				r.Class = strings.ToUpper(strings.TrimSpace(r.Class))
				if !validClass(r.Class) {
					r.Class = "KEEP"
				}
				ordered[i] = r
				found = true
				break
			}
		}
		if !found {
			ordered[i] = batchClassification{
				Class:  "KEEP",
				Reason: "not returned in batch — defaulting to KEEP",
			}
		}
	}

	return ordered
}

// classifyFallback processes items one at a time if batch fails.
func classifyFallback(items []batchItem) []batchClassification {
	results := make([]batchClassification, len(items))
	for i, item := range items {
		class, reason := classifyItemSingle(item.Source, item.Title, item.Content)
		results[i] = batchClassification{Class: class, Reason: reason}
	}
	return results
}

func classifyItemSingle(source, title, content string) (string, string) {
	prompt := fmt.Sprintf(`Classify this bookmarked item for a personal AI system focused on:
- AI infrastructure, local models (MLX, quantization, training)
- Apple development (iOS, macOS, Swift)
- Security monitoring, revenue generation, indie hacking
- MODUS product development

Source: %s
Title: %s
Content: %s

Classify as ONE of: ADAPT (actionable), KEEP (useful reference), MORE_INFO (need full article), DISCARD (noise).
Return ONLY the classification word followed by a colon and one-sentence reason.`, source, title, truncate(content, 500))

	text := callLibrarian("Classify content. Return only the label and reason.", prompt, 100)

	for _, cls := range []string{"ADAPT", "KEEP", "MORE_INFO", "DISCARD"} {
		if strings.HasPrefix(strings.ToUpper(text), cls) {
			reason := text
			if idx := strings.Index(text, ":"); idx >= 0 {
				reason = strings.TrimSpace(text[idx+1:])
			}
			return cls, reason
		}
	}
	return "KEEP", text
}

// ── Dialectic Challenge ──────────────────────────────────────────────

// dialecticResult holds the outcome of a challenge loop.
type dialecticResult struct {
	Survived    bool
	Rounds      int
	FinalClass  string
	FinalReason string
}

// dialecticChallenge stress-tests an ADAPT classification.
// The librarian argues against its own classification, then defends it.
// If the item survives maxChallengeRounds, it's confirmed ADAPT.
// If demoted, it becomes KEEP or DISCARD with the challenger's reasoning.
func dialecticChallenge(title, content, initialReason string) dialecticResult {
	context := fmt.Sprintf("Title: %s\nContent: %s\nInitial assessment: ADAPT — %s",
		title, truncate(content, 500), initialReason)

	for round := 1; round <= maxChallengeRounds; round++ {
		// Challenger: argue AGAINST the ADAPT classification
		challengePrompt := fmt.Sprintf(`You are a skeptical auditor. An item was classified as ADAPT (directly actionable) for a developer building AI infrastructure on Apple Silicon.

%s

Challenge round %d/%d. Argue why this should NOT be ADAPT. Consider:
- Is this actually actionable or just interesting?
- Do we already have this capability?
- Is the effort worth the payoff?
- Is this hype or substance?

If the classification is wrong, state: DEMOTE: [KEEP or DISCARD] — reason
If you cannot find a strong argument against it, state: SURVIVES — reason it's genuinely actionable

Return ONLY one line: DEMOTE or SURVIVES followed by the reason.`, context, round, maxChallengeRounds)

		response := callLibrarian(
			"You are a skeptical auditor. Challenge weak classifications ruthlessly. Be concise.",
			challengePrompt, 150)

		upper := strings.ToUpper(response)

		if strings.HasPrefix(upper, "SURVIVES") {
			reason := initialReason
			if idx := strings.Index(response, "—"); idx >= 0 {
				reason = strings.TrimSpace(response[idx+3:]) // skip "— "
			} else if idx := strings.Index(response, "-"); idx >= 0 {
				reason = strings.TrimSpace(response[idx+1:])
			}
			return dialecticResult{
				Survived:    true,
				Rounds:      round,
				FinalClass:  "ADAPT",
				FinalReason: fmt.Sprintf("[challenged %d/%d] %s", round, maxChallengeRounds, reason),
			}
		}

		if strings.HasPrefix(upper, "DEMOTE") {
			// Extract the target class and reason
			targetClass := "KEEP"
			reason := response
			if idx := strings.Index(response, ":"); idx >= 0 {
				after := strings.TrimSpace(response[idx+1:])
				for _, cls := range []string{"DISCARD", "KEEP"} {
					if strings.HasPrefix(strings.ToUpper(after), cls) {
						targetClass = cls
						if dashIdx := strings.Index(after, "—"); dashIdx >= 0 {
							reason = strings.TrimSpace(after[dashIdx+3:])
						} else if dashIdx := strings.Index(after, "-"); dashIdx >= 0 {
							reason = strings.TrimSpace(after[dashIdx+1:])
						} else {
							reason = after
						}
						break
					}
				}
			}

			// Defender gets one chance to rebut
			defensePrompt := fmt.Sprintf(`You are defending the ADAPT classification.

%s

The challenger says: %s

Defend in one sentence why this IS actionable and should remain ADAPT.
Or concede: CONCEDE — the challenger is right.

Return ONLY: DEFEND — reason  OR  CONCEDE — reason`, context, response)

			defense := callLibrarian(
				"You defend classifications. Be honest — concede if the challenger is right.",
				defensePrompt, 100)

			defenseUpper := strings.ToUpper(defense)
			if strings.HasPrefix(defenseUpper, "CONCEDE") {
				return dialecticResult{
					Survived:    false,
					Rounds:      round,
					FinalClass:  targetClass,
					FinalReason: fmt.Sprintf("[demoted round %d] %s", round, reason),
				}
			}

			// Defense held — update context for next round
			context = fmt.Sprintf("%s\nRound %d — Challenge: %s | Defense: %s",
				context, round, truncate(response, 100), truncate(defense, 100))
			continue
		}

		// Ambiguous response — treat as survived this round
		continue
	}

	// Survived all rounds without explicit SURVIVES — confirmed
	return dialecticResult{
		Survived:    true,
		Rounds:      maxChallengeRounds,
		FinalClass:  "ADAPT",
		FinalReason: fmt.Sprintf("[survived %d challenges] %s", maxChallengeRounds, initialReason),
	}
}

// ── Librarian Client ─────────────────────────────────────────────────

func librarianAvailable() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	// MLX server doesn't have /health — check /v1/models instead
	resp, err := client.Get(librarianEndpoint + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func callLibrarian(system, user string, maxTokens int) string {
	reqBody := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":          maxTokens,
		"temperature":         0.1,
		"chat_template_kwargs": map[string]interface{}{"enable_thinking": false},
	}

	jsonBody, _ := json.Marshal(reqBody)
	client := &http.Client{Timeout: 120 * time.Second}

	resp, err := client.Post(
		librarianEndpoint+"/v1/chat/completions",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		log.Printf("wraith/librarian: call failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil || len(result.Choices) == 0 {
		log.Printf("wraith/librarian: parse failed: %v", err)
		return ""
	}

	return strings.TrimSpace(result.Choices[0].Message.Content)
}

// ── Helpers ──────────────────────────────────────────────────────────

func extractBody(raw string) string {
	if idx := strings.Index(raw, "---\n"); idx >= 0 {
		if idx2 := strings.Index(raw[idx+4:], "---\n"); idx2 >= 0 {
			return raw[idx+4+idx2+4:]
		}
	}
	return raw
}

func stripFences(text string) string {
	for _, stop := range []string{"<|user|>", "<|endoftext|>", "<|im_end|>", "<|assistant|>"} {
		if idx := strings.Index(text, stop); idx > 0 {
			text = text[:idx]
		}
	}
	clean := strings.TrimSpace(text)
	if strings.HasPrefix(clean, "```") {
		lines := strings.SplitN(clean, "\n", 2)
		if len(lines) > 1 {
			clean = lines[1]
		}
	}
	if strings.HasSuffix(clean, "```") {
		clean = clean[:len(clean)-3]
	}
	return strings.TrimSpace(clean)
}

func validClass(c string) bool {
	switch c {
	case "ADAPT", "KEEP", "MORE_INFO", "DISCARD":
		return true
	}
	return false
}
