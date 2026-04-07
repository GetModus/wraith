package wraith

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestFixtureReplayDedup replays all capture fixtures from the wraith/fixtures/
// directory through the queue and verifies dedup behavior.
// This test is the machine-verifiable contract for fixture-based regression.
func TestFixtureReplayDedup(t *testing.T) {
	// Locate fixtures relative to repo root
	fixturesDir := locateFixtures(t)
	if fixturesDir == "" {
		t.Skip("fixtures directory not found — skipping fixture replay")
	}

	capturesDir := filepath.Join(fixturesDir, "captures")
	entries, err := os.ReadDir(capturesDir)
	if err != nil {
		t.Fatalf("read captures dir: %v", err)
	}

	// Sort for deterministic ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	q, err := OpenQueue(t.TempDir())
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(capturesDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var cap Capture
		if err := json.Unmarshal(data, &cap); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		if err := q.Enqueue(&cap); err != nil {
			t.Fatalf("enqueue %s: %v", entry.Name(), err)
		}
		t.Logf("enqueued: %-30s -> status=%s", entry.Name(), cap.Status)
	}

	stats := q.Stats()
	t.Logf("queue stats: total=%d queued=%d deduped=%d", stats.Total, stats.Queued, stats.Deduped)

	if stats.Total < 9 {
		t.Errorf("expected at least 9 total captures, got %d", stats.Total)
	}

	// duplicate-exact-b should be deduped (same fingerprint as duplicate-exact-a)
	// duplicate-near-b should be near-deduped (Jaccard ≥0.82 with duplicate-near-a)
	if stats.Deduped < 1 {
		t.Errorf("expected at least 1 deduped capture, got %d", stats.Deduped)
	}
}

// locateFixtures walks up from the test file to find fixtures/.
func locateFixtures(t *testing.T) string {
	t.Helper()
	// In standalone repo: internal/wraith/ → fixtures is at ../../fixtures/
	// In monorepo: go/internal/wraith/ → fixtures at ../../../wraith/fixtures/
	candidates := []string{
		"../../fixtures",              // standalone repo (internal/wraith/ → root)
		"../../../wraith/fixtures",    // monorepo (go/internal/wraith/)
		"../../wraith/fixtures",       // monorepo fallback
		"../../../../wraith/fixtures", // deeper nesting
	}
	for _, c := range candidates {
		abs, _ := filepath.Abs(c)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}
