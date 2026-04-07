package wraith

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// State tracks ingested items for dedup and triage. JSON file on disk, no SQLite.
type State struct {
	mu      sync.RWMutex
	path    string
	records map[string]*IngestionRecord // key: "source|external_id"
}

// OpenState opens (or creates) the ingestion state file.
func OpenState(dataDir string) (*State, error) {
	os.MkdirAll(dataDir, 0755)
	path := filepath.Join(dataDir, "wraith-state.json")

	s := &State{
		path:    path,
		records: make(map[string]*IngestionRecord),
	}

	// Load existing state
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		var records []*IngestionRecord
		if err := json.Unmarshal(data, &records); err != nil {
			log.Printf("wraith: state parse error (starting fresh): %v", err)
		} else {
			for _, r := range records {
				s.records[r.key()] = r
			}
		}
	}

	return s, nil
}

// Exists checks if an item has already been ingested.
func (s *State) Exists(source, externalID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[stateKey(source, externalID)]
	return ok
}

// Record stores a newly ingested item and persists to disk.
func (s *State) Record(source, externalID, url, title, vaultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := stateKey(source, externalID)
	if _, ok := s.records[key]; ok {
		return nil // already exists
	}

	s.records[key] = &IngestionRecord{
		Source:     source,
		ExternalID: externalID,
		URL:        url,
		Title:      title,
		VaultPath:  vaultPath,
		IngestedAt: time.Now().UTC().Format(time.RFC3339),
		Triage:     "pending",
	}

	return s.save()
}

// SetTriage updates the triage classification for an item and persists.
func (s *State) SetTriage(source, externalID, triage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := stateKey(source, externalID)
	r, ok := s.records[key]
	if !ok {
		return fmt.Errorf("item not found: %s/%s", source, externalID)
	}

	r.Triage = triage
	return s.save()
}

// PendingTriage returns items that haven't been triaged yet.
func (s *State) PendingTriage(limit int) ([]IngestionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var pending []IngestionRecord
	for _, r := range s.records {
		if r.Triage == "pending" {
			pending = append(pending, *r)
		}
	}

	// Sort by ingested time, newest first
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].IngestedAt > pending[j].IngestedAt
	})

	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending, nil
}

// Stats returns ingestion statistics grouped by source.
func (s *State) Stats() ([]SourceStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bySource := make(map[string]*SourceStats)

	for _, r := range s.records {
		st, ok := bySource[r.Source]
		if !ok {
			st = &SourceStats{Source: r.Source}
			bySource[r.Source] = st
		}
		st.Total++
		switch r.Triage {
		case "pending":
			st.Pending++
		case "ADAPT":
			st.Adapt++
		case "KEEP":
			st.Keep++
		case "DISCARD":
			st.Discard++
		}
		if r.IngestedAt > st.LastIngested {
			st.LastIngested = r.IngestedAt
		}
	}

	var stats []SourceStats
	for _, st := range bySource {
		stats = append(stats, *st)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].LastIngested > stats[j].LastIngested
	})

	return stats, nil
}

// Close is a no-op — state is persisted on every write.
func (s *State) Close() error {
	return nil
}

// Count returns total number of tracked items.
func (s *State) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// save writes the current state to disk. Must be called with mu held.
func (s *State) save() error {
	records := make([]*IngestionRecord, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, r)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	return os.WriteFile(s.path, data, 0644)
}

// IngestionRecord represents a single ingested item.
type IngestionRecord struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title"`
	VaultPath  string `json:"vault_path,omitempty"`
	IngestedAt string `json:"ingested_at"`
	Triage     string `json:"triage"`
}

func (r *IngestionRecord) key() string {
	return stateKey(r.Source, r.ExternalID)
}

// SourceStats holds per-source ingestion statistics.
type SourceStats struct {
	Source       string `json:"source"`
	Total        int    `json:"total"`
	Pending      int    `json:"pending"`
	Adapt        int    `json:"adapt"`
	Keep         int    `json:"keep"`
	Discard      int    `json:"discard"`
	LastIngested string `json:"last_ingested"`
}

func stateKey(source, externalID string) string {
	return source + "|" + externalID
}
