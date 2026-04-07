package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GetModus/wraith/internal/wraith"
)

func TestLoadHandoffStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataDir := filepath.Join(home, "modus", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	// Seed handoff ledger.
	handoffPath := filepath.Join(dataDir, "wraith-officer-handoffs.jsonl")
	content := `{"capture_id":"cap-1","scout":{"class":"keep","at":"2026-04-07T10:00:00Z"},"librarian":{"at":"2026-04-07T10:00:01Z"}}
{"capture_id":"cap-2","scout":{"class":"discard","at":"2026-04-07T10:02:00Z"}}
`
	if err := os.WriteFile(handoffPath, []byte(content), 0644); err != nil {
		t.Fatalf("write handoff file: %v", err)
	}

	stats, err := loadHandoffStats()
	if err != nil {
		t.Fatalf("loadHandoffStats: %v", err)
	}
	if !stats.Available {
		t.Fatalf("available = false, want true")
	}
	if stats.Total != 2 {
		t.Fatalf("total = %d, want 2", stats.Total)
	}
	if stats.WithLibrarian != 1 {
		t.Fatalf("with_librarian = %d, want 1", stats.WithLibrarian)
	}
	if stats.ByClass["keep"] != 1 || stats.ByClass["discard"] != 1 {
		t.Fatalf("by_class unexpected: %+v", stats.ByClass)
	}
}

func TestLoadHandoffStatsMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stats, err := loadHandoffStats()
	if err != nil {
		t.Fatalf("loadHandoffStats: %v", err)
	}
	if stats.Available {
		t.Fatalf("available = true, want false")
	}
	if stats.Total != 0 {
		t.Fatalf("total = %d, want 0", stats.Total)
	}
}

func TestNewWraithBridge(t *testing.T) {
	dataDir := t.TempDir()
	q, err := wraith.OpenQueue(dataDir)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}

	b := NewWraithBridge(q)
	if b == nil {
		t.Fatalf("bridge is nil")
	}

	status := b.GetStatus()
	data, _ := json.Marshal(status)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if m["connected"] != false {
		t.Fatalf("new bridge should not be connected")
	}
}
