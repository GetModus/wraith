package mcp

import (
	"encoding/json"
	"testing"
)

func TestRegisterWraithToolsCaptureAndQueue(t *testing.T) {
	vaultDir := t.TempDir()
	dataDir := t.TempDir()

	srv := NewServer("test", "0")
	RegisterWraithTools(srv, vaultDir, dataDir)

	if !srv.HasTool("wraith_capture") || !srv.HasTool("wraith_queue") || !srv.HasTool("wraith_status") {
		t.Fatalf("expected WRAITH MCP tools to be registered")
	}

	captureResult, err := srv.CallTool("wraith_capture", map[string]interface{}{
		"source":    "api",
		"url":       "https://example.com/modular-wraith",
		"title":     "Modular WRAITH",
		"body_text": "queue this through the neutral interface",
	})
	if err != nil {
		t.Fatalf("wraith_capture: %v", err)
	}

	var capturePayload map[string]interface{}
	if err := json.Unmarshal([]byte(captureResult), &capturePayload); err != nil {
		t.Fatalf("parse capture result: %v", err)
	}
	if got := capturePayload["status"]; got != "queued" {
		t.Fatalf("capture status = %v, want queued", got)
	}
	if _, ok := capturePayload["id"].(string); !ok {
		t.Fatalf("expected capture id in result")
	}

	queueResult, err := srv.CallTool("wraith_queue", map[string]interface{}{
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("wraith_queue: %v", err)
	}

	var queuePayload struct {
		Pending []map[string]interface{} `json:"pending"`
		Stats   map[string]interface{}   `json:"stats"`
	}
	if err := json.Unmarshal([]byte(queueResult), &queuePayload); err != nil {
		t.Fatalf("parse queue result: %v", err)
	}
	if len(queuePayload.Pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(queuePayload.Pending))
	}
	if got := queuePayload.Pending[0]["url"]; got != "https://example.com/modular-wraith" {
		t.Fatalf("pending[0].url = %v", got)
	}
}

func TestRegisterWraithToolsProcessAndSources(t *testing.T) {
	vaultDir := t.TempDir()
	dataDir := t.TempDir()

	srv := NewServer("test", "0")
	RegisterWraithTools(srv, vaultDir, dataDir)

	_, err := srv.CallTool("wraith_capture", map[string]interface{}{
		"source":    "api",
		"url":       "https://example.com/process-me",
		"title":     "Process Me",
		"body_text": "retain this capture and file it",
	})
	if err != nil {
		t.Fatalf("wraith_capture: %v", err)
	}

	processResult, err := srv.CallTool("wraith_process", map[string]interface{}{
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("wraith_process: %v", err)
	}

	var processPayload map[string]interface{}
	if err := json.Unmarshal([]byte(processResult), &processPayload); err != nil {
		t.Fatalf("parse process result: %v", err)
	}
	if got := int(processPayload["processed"].(float64)); got != 1 {
		t.Fatalf("processed = %d, want 1", got)
	}

	sourcesResult, err := srv.CallTool("wraith_sources", map[string]interface{}{})
	if err != nil {
		t.Fatalf("wraith_sources: %v", err)
	}
	var sourcesPayload struct {
		Sources []struct {
			Source string `json:"source"`
			Total  int    `json:"total"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(sourcesResult), &sourcesPayload); err != nil {
		t.Fatalf("parse sources result: %v", err)
	}
	if len(sourcesPayload.Sources) == 0 {
		t.Fatalf("expected at least one source stat")
	}
	if got := sourcesPayload.Sources[0].Source; got != "extension-api" {
		t.Fatalf("source = %q, want extension-api", got)
	}
}
