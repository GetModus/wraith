package wraith

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingLibrarianOfficer struct{}

func (f failingLibrarianOfficer) File(vaultDir string, cap *Capture, frontmatter map[string]interface{}, body string) (FilingReceipt, error) {
	return FilingReceipt{}, errors.New("simulated librarian write failure")
}

func TestAcceptanceCaptureToQueueScoutLibrarianFile(t *testing.T) {
	dataDir := t.TempDir()
	vaultDir := t.TempDir()

	queue, err := OpenQueue(dataDir)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	state, err := OpenState(dataDir)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}

	cap := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/security",
		Title:    "Security Design Note",
		BodyText: "This note should be filed by librarian after scout assessment.",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 10, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "acceptance keep"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1", n)
	}
	if lib.called != 1 {
		t.Fatalf("librarian calls = %d, want 1", lib.called)
	}

	got := queue.captures[0]
	if got.Status != "done" {
		t.Fatalf("status = %q, want done", got.Status)
	}
	if got.VaultPath == "" {
		t.Fatalf("vault_path empty")
	}
	if _, err := os.Stat(got.VaultPath); err != nil {
		t.Fatalf("expected filed markdown at %s: %v", got.VaultPath, err)
	}

	source := captureSource(got)
	externalID := captureExternalID(got)
	if !state.Exists(source, externalID) {
		t.Fatalf("state missing ingestion record for %s/%s", source, externalID)
	}

	handoffPath := filepath.Join(dataDir, "wraith-officer-handoffs.jsonl")
	if _, err := os.Stat(handoffPath); err != nil {
		t.Fatalf("expected handoff ledger file: %v", err)
	}
}

func TestAcceptanceFailurePathMarksFailed(t *testing.T) {
	dataDir := t.TempDir()
	vaultDir := t.TempDir()

	queue, err := OpenQueue(dataDir)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	state, err := OpenState(dataDir)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}

	cap := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/failure",
		Title:    "Failure Case",
		BodyText: "This should fail at librarian write stage.",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 10, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "acceptance keep"},
		Librarian: failingLibrarianOfficer{},
	})
	if err != nil {
		t.Fatalf("process queue returned error: %v", err)
	}
	if n != 0 {
		t.Fatalf("written count = %d, want 0", n)
	}

	got := queue.captures[0]
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.IngestState != "failed" {
		t.Fatalf("ingest_state = %q, want failed", got.IngestState)
	}
	if got.Error == "" {
		t.Fatalf("expected queue error detail on failed capture")
	}
}

func TestAcceptanceStateDuplicateGetsDeduped(t *testing.T) {
	dataDir := t.TempDir()
	vaultDir := t.TempDir()

	queue, err := OpenQueue(dataDir)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	state, err := OpenState(dataDir)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}

	// Pre-seed state with an item using capture source/external id mapping.
	if err := state.Record("extension-context-menu", "https://example.com/dupe", "https://example.com/dupe", "Existing", "brain/captures/existing.md"); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	cap := &Capture{
		Source:   "context-menu",
		URL:      "https://example.com/dupe",
		Title:    "Duplicate Item",
		BodyText: "Should be deduped by state",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 10, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "acceptance keep"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 0 {
		t.Fatalf("written count = %d, want 0", n)
	}
	if lib.called != 0 {
		t.Fatalf("librarian should not be called for state duplicate, called %d", lib.called)
	}
	if queue.captures[0].Status != "deduped" {
		t.Fatalf("status = %q, want deduped", queue.captures[0].Status)
	}
}
