package wraith

import (
	"testing"
)

func TestQueueEnqueueIngressDedup(t *testing.T) {
	q, err := OpenQueue(t.TempDir())
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}

	first := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/a",
		Title:    "Example Capture",
		BodyText: "alpha body",
	}
	if err := q.Enqueue(first); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}

	dup := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/a/",
		Title:    "Example Capture",
		BodyText: "alpha body",
	}
	if err := q.Enqueue(dup); err != nil {
		t.Fatalf("enqueue dup: %v", err)
	}

	if len(q.captures) != 2 {
		t.Fatalf("expected 2 capture records (canonical + dedup trace), got %d", len(q.captures))
	}

	if q.captures[0].DuplicateCount != 1 {
		t.Fatalf("expected canonical duplicate_count=1, got %d", q.captures[0].DuplicateCount)
	}
	if q.captures[1].Status != "deduped" {
		t.Fatalf("expected deduped status for duplicate capture, got %s", q.captures[1].Status)
	}
	if q.captures[1].DuplicateOf == "" || q.captures[1].DuplicateOf != q.captures[0].ID {
		t.Fatalf("expected duplicate_of=%s, got %s", q.captures[0].ID, q.captures[1].DuplicateOf)
	}
	if q.captures[1].IngestState != "deduped" {
		t.Fatalf("expected ingest_state deduped, got %s", q.captures[1].IngestState)
	}
}

func TestQueueStatusTransitionsUpdateIngestLedger(t *testing.T) {
	q, err := OpenQueue(t.TempDir())
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}

	c := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/b",
		Title:    "Transition Capture",
		BodyText: "beta",
	}
	if err := q.Enqueue(c); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id := q.captures[0].ID

	if err := q.SetStatus(id, "processing", ""); err != nil {
		t.Fatalf("set processing: %v", err)
	}
	if err := q.SetVaultPath(id, "brain/captures/2026-04-07-transition-capture.md"); err != nil {
		t.Fatalf("set vault path: %v", err)
	}
	if err := q.SetStatus(id, "done", ""); err != nil {
		t.Fatalf("set done: %v", err)
	}

	got := q.captures[0]
	if got.IngestState != "filed" {
		t.Fatalf("expected ingest_state filed, got %s", got.IngestState)
	}
	if len(got.IngestHistory) < 3 {
		t.Fatalf("expected ingest history entries, got %d", len(got.IngestHistory))
	}
}

func TestQueueEnqueueNearDuplicateFold(t *testing.T) {
	q, err := OpenQueue(t.TempDir())
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}

	first := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/article?id=1",
		Title:    "Agent Architecture Notes",
		BodyText: "Modus uses a queue ledger for capture ingestion and officer handoff evidence.",
	}
	if err := q.Enqueue(first); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}

	near := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/article?id=1",
		Title:    "Agent architecture notes",
		BodyText: "Modus uses queue ledger for capture ingestion plus officer handoff evidence and audit.",
	}
	if err := q.Enqueue(near); err != nil {
		t.Fatalf("enqueue near duplicate: %v", err)
	}

	if len(q.captures) != 2 {
		t.Fatalf("expected 2 capture records, got %d", len(q.captures))
	}
	canonical := q.captures[0]
	dup := q.captures[1]
	if dup.Status != "deduped" {
		t.Fatalf("expected deduped status, got %s", dup.Status)
	}
	if dup.DuplicateOf != canonical.ID {
		t.Fatalf("expected duplicate_of=%s, got %s", canonical.ID, dup.DuplicateOf)
	}
	if canonical.DuplicateCount != 1 {
		t.Fatalf("expected canonical duplicate_count=1, got %d", canonical.DuplicateCount)
	}
}
