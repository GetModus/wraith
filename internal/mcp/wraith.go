package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GetModus/wraith/internal/wraith"
)

// RegisterWraithTools adds a neutral MCP control surface for WRAITH so other
// harnesses can enqueue captures, inspect queue state, and process intake
// without depending on the Safari bridge directly.
func RegisterWraithTools(srv *Server, vaultDir, dataDir string) {
	if strings.TrimSpace(vaultDir) == "" {
		return
	}
	dataDir = resolveWraithDataDir(dataDir)

	srv.AddTool("wraith_status", "Return WRAITH queue statistics and source-level ingest counts.", map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}, func(args map[string]interface{}) (string, error) {
		queue, err := wraith.OpenQueue(dataDir)
		if err != nil {
			return "", err
		}
		state, err := wraith.OpenState(dataDir)
		if err != nil {
			return "", err
		}
		defer state.Close()

		payload := map[string]interface{}{
			"queue":        queue.Stats(),
			"source_stats": mustWraithSourceStats(state),
			"total_items":  state.Count(),
			"data_dir":     dataDir,
			"vault_dir":    vaultDir,
		}
		return marshalIndented(payload)
	})

	srv.AddTool("wraith_queue", "Inspect queued WRAITH captures waiting for processing.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"limit": map[string]interface{}{"type": "integer", "description": "Maximum queued captures to return (default 20)"},
		},
	}, func(args map[string]interface{}) (string, error) {
		queue, err := wraith.OpenQueue(dataDir)
		if err != nil {
			return "", err
		}
		limit := 20
		if l, ok := args["limit"].(float64); ok && int(l) > 0 {
			limit = int(l)
		}
		payload := map[string]interface{}{
			"pending": queue.Pending(limit),
			"stats":   queue.Stats(),
		}
		return marshalIndented(payload)
	})

	srv.AddTool("wraith_capture", "Enqueue a WRAITH capture directly. Useful for non-browser harnesses that want to submit content into the intake queue.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"source":    map[string]interface{}{"type": "string", "description": "Capture source label (e.g. api, context-menu, bookmark-sync)"},
			"url":       map[string]interface{}{"type": "string", "description": "Canonical URL for the capture"},
			"title":     map[string]interface{}{"type": "string", "description": "Capture title"},
			"site_name": map[string]interface{}{"type": "string", "description": "Optional site name"},
			"author":    map[string]interface{}{"type": "string", "description": "Optional author"},
			"body_text": map[string]interface{}{"type": "string", "description": "Optional extracted body text"},
			"selected":  map[string]interface{}{"type": "string", "description": "Optional selected text"},
			"meta":      map[string]interface{}{"type": "object", "description": "Optional metadata object"},
			"tweet":     map[string]interface{}{"type": "object", "description": "Optional X/Twitter payload matching CaptureTweet"},
		},
		"required": []string{"source", "url"},
	}, func(args map[string]interface{}) (string, error) {
		queue, err := wraith.OpenQueue(dataDir)
		if err != nil {
			return "", err
		}

		cap := &wraith.Capture{
			Source:   stringArg(args, "source"),
			URL:      stringArg(args, "url"),
			Title:    stringArg(args, "title"),
			SiteName: stringArg(args, "site_name"),
			Author:   stringArg(args, "author"),
			BodyText: stringArg(args, "body_text"),
			Selected: stringArg(args, "selected"),
		}
		if cap.Meta == nil {
			if meta, ok := args["meta"].(map[string]interface{}); ok {
				cap.Meta = meta
			}
		}
		if tweetRaw, ok := args["tweet"].(map[string]interface{}); ok {
			var tweet wraith.CaptureTweet
			buf, err := json.Marshal(tweetRaw)
			if err != nil {
				return "", fmt.Errorf("marshal tweet payload: %w", err)
			}
			if err := json.Unmarshal(buf, &tweet); err != nil {
				return "", fmt.Errorf("parse tweet payload: %w", err)
			}
			cap.Tweet = &tweet
		}

		if strings.TrimSpace(cap.Source) == "" || strings.TrimSpace(cap.URL) == "" {
			return "", fmt.Errorf("source and url are required")
		}
		if err := queue.Enqueue(cap); err != nil {
			return "", err
		}

		payload := map[string]interface{}{
			"id":           cap.ID,
			"status":       cap.Status,
			"ingest_state": cap.IngestState,
			"fingerprint":  cap.Fingerprint,
			"duplicate_of": cap.DuplicateOf,
		}
		return marshalIndented(payload)
	})

	srv.AddTool("wraith_process", "Process queued WRAITH captures through YouTube routing, Scout, and Librarian filing.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"limit": map[string]interface{}{"type": "integer", "description": "Maximum queued captures to process (default 20)"},
		},
	}, func(args map[string]interface{}) (string, error) {
		queue, err := wraith.OpenQueue(dataDir)
		if err != nil {
			return "", err
		}
		state, err := wraith.OpenState(dataDir)
		if err != nil {
			return "", err
		}
		defer state.Close()

		limit := 20
		if l, ok := args["limit"].(float64); ok && int(l) > 0 {
			limit = int(l)
		}

		n, err := wraith.ProcessQueue(queue, state, vaultDir, limit)
		if err != nil {
			return "", err
		}
		payload := map[string]interface{}{
			"processed":    n,
			"queue":        queue.Stats(),
			"source_stats": mustWraithSourceStats(state),
		}
		return marshalIndented(payload)
	})

	srv.AddTool("wraith_sources", "Return source-level WRAITH ingest statistics from state.", map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}, func(args map[string]interface{}) (string, error) {
		state, err := wraith.OpenState(dataDir)
		if err != nil {
			return "", err
		}
		defer state.Close()

		payload := map[string]interface{}{
			"sources":     mustWraithSourceStats(state),
			"total_items": state.Count(),
		}
		return marshalIndented(payload)
	})
}

func resolveWraithDataDir(dataDir string) string {
	if strings.TrimSpace(dataDir) != "" {
		return dataDir
	}
	if envDir := os.Getenv("MODUS_DATA_DIR"); strings.TrimSpace(envDir) != "" {
		return envDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "modus", "data")
}

func mustWraithSourceStats(state *wraith.State) interface{} {
	stats, err := state.Stats()
	if err != nil {
		return []wraith.SourceStats{}
	}
	return stats
}

func marshalIndented(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stringArg(args map[string]interface{}, key string) string {
	val, _ := args[key].(string)
	return strings.TrimSpace(val)
}
