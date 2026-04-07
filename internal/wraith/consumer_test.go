package wraith

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testScoutOfficer struct {
	class  string
	reason string
}

func (s testScoutOfficer) Assess(_ *Capture) ScoutAssessment {
	return ScoutAssessment{
		Class:   s.class,
		Reason:  s.reason,
		Officer: "scout",
		Model:   "test-scout-model",
		At:      "2026-04-07T00:00:00Z",
	}
}

type testLibrarianOfficer struct {
	called int
}

func (l *testLibrarianOfficer) File(vaultDir string, cap *Capture, _ map[string]interface{}, body string) (FilingReceipt, error) {
	l.called++
	path := filepath.Join(vaultDir, "brain", "captures", "test-"+cap.ID+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return FilingReceipt{}, err
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return FilingReceipt{}, err
	}
	return FilingReceipt{
		VaultPath: path,
		Officer:   "librarian",
		Model:     "test-librarian-model",
		Checksum:  "abc123",
		At:        "2026-04-07T00:00:01Z",
	}, nil
}

func TestProcessQueueWithOfficersDiscardSkipsLibrarian(t *testing.T) {
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
		URL:      "https://example.com/empty",
		Title:    "Empty",
		BodyText: "discard me",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     testScoutOfficer{class: "discard", reason: "test discard"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 0 {
		t.Fatalf("written count = %d, want 0", n)
	}
	if lib.called != 0 {
		t.Fatalf("librarian called %d times, want 0", lib.called)
	}
	if queue.captures[0].Status != "discarded" {
		t.Fatalf("status = %q, want discarded", queue.captures[0].Status)
	}

	handoffPath := filepath.Join(dataDir, "wraith-officer-handoffs.jsonl")
	data, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("read handoff file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("handoff line count = %d, want 1", len(lines))
	}
	var rec OfficerHandoffRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("parse handoff line: %v", err)
	}
	if rec.Librarian != nil {
		t.Fatalf("expected no librarian receipt for discarded item")
	}
	if rec.Scout.Class != "discard" {
		t.Fatalf("scout class = %q, want discard", rec.Scout.Class)
	}
}

func TestProcessQueueWithOfficersKeepFilesViaLibrarian(t *testing.T) {
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
		URL:      "https://example.com/keep",
		Title:    "Keep",
		BodyText: "retain me",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "test keep"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1", n)
	}
	if lib.called != 1 {
		t.Fatalf("librarian called %d times, want 1", lib.called)
	}
	if queue.captures[0].Status != "done" {
		t.Fatalf("status = %q, want done", queue.captures[0].Status)
	}
	if queue.captures[0].VaultPath == "" {
		t.Fatalf("expected vault path after librarian filing")
	}

	handoffPath := filepath.Join(dataDir, "wraith-officer-handoffs.jsonl")
	data, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("read handoff file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("handoff line count = %d, want 1", len(lines))
	}
	var rec OfficerHandoffRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("parse handoff line: %v", err)
	}
	if rec.Librarian == nil {
		t.Fatalf("expected librarian receipt on keep")
	}
	if rec.Librarian.Officer != "librarian" {
		t.Fatalf("librarian officer = %q, want librarian", rec.Librarian.Officer)
	}
}

func TestProcessQueueRoutesYouTubeVideoDirectly(t *testing.T) {
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
		Source: "context-menu",
		URL:    "https://www.youtube.com/watch?v=YpPcDHc3e9U",
		Title:  "YT video",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	origVideo := ingestYouTubeVideoFn
	origPlaylist := ingestYouTubeFn
	t.Cleanup(func() {
		ingestYouTubeVideoFn = origVideo
		ingestYouTubeFn = origPlaylist
	})

	var calledVideo string
	ingestYouTubeVideoFn = func(vaultDir string, state *State, videoURL string) (string, error) {
		calledVideo = videoURL
		return filepath.Join(vaultDir, "sources", "youtube", "stub.md"), nil
	}
	ingestYouTubeFn = func(_ string, _ *State, _ []string) (int, error) {
		t.Fatalf("playlist ingest should not be called for video URL")
		return 0, nil
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "unused"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1", n)
	}
	if calledVideo == "" {
		t.Fatalf("expected youtube video ingest to be called")
	}
	if lib.called != 0 {
		t.Fatalf("librarian officer called %d times, want 0", lib.called)
	}
	if queue.captures[0].Status != "done" {
		t.Fatalf("status = %q, want done", queue.captures[0].Status)
	}
	if queue.captures[0].VaultPath == "" {
		t.Fatalf("expected vault path to be recorded")
	}
}

func TestProcessQueuePrefersYouTubeVideoOverPlaylistContext(t *testing.T) {
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
		Source: "context-menu",
		URL:    "https://www.youtube.com/watch?v=YpPcDHc3e9U&list=PLdRJDipD_1ByyyCw8526yePJ98adIpFbQ",
		Title:  "YT video in playlist context",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	origVideo := ingestYouTubeVideoFn
	origPlaylist := ingestYouTubeFn
	t.Cleanup(func() {
		ingestYouTubeVideoFn = origVideo
		ingestYouTubeFn = origPlaylist
	})

	var calledVideo string
	ingestYouTubeVideoFn = func(vaultDir string, state *State, videoURL string) (string, error) {
		calledVideo = videoURL
		return filepath.Join(vaultDir, "sources", "youtube", "video-in-playlist.md"), nil
	}
	ingestYouTubeFn = func(_ string, _ *State, _ []string) (int, error) {
		t.Fatalf("playlist ingest should not be called for a watch URL")
		return 0, nil
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "unused"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1", n)
	}
	if calledVideo == "" {
		t.Fatalf("expected youtube video ingest to be called")
	}
	if lib.called != 0 {
		t.Fatalf("librarian officer called %d times, want 0", lib.called)
	}
	if queue.captures[0].Status != "done" {
		t.Fatalf("status = %q, want done", queue.captures[0].Status)
	}
}

func TestProcessQueueRoutesYouTubePlaylistDirectly(t *testing.T) {
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
		Source: "context-menu",
		URL:    "https://www.youtube.com/playlist?list=PLdRJDipD_1ByyyCw8526yePJ98adIpFbQ",
		Title:  "YT playlist",
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	origVideo := ingestYouTubeVideoFn
	origPlaylist := ingestYouTubeFn
	t.Cleanup(func() {
		ingestYouTubeVideoFn = origVideo
		ingestYouTubeFn = origPlaylist
	})

	var calledPlaylists []string
	ingestYouTubeVideoFn = func(_ string, _ *State, _ string) (string, error) {
		t.Fatalf("video ingest should not be called for playlist URL")
		return "", nil
	}
	ingestYouTubeFn = func(_ string, _ *State, playlistIDs []string) (int, error) {
		calledPlaylists = append([]string(nil), playlistIDs...)
		return 3, nil
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     testScoutOfficer{class: "keep", reason: "unused"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1", n)
	}
	if len(calledPlaylists) != 1 || calledPlaylists[0] != "PLdRJDipD_1ByyyCw8526yePJ98adIpFbQ" {
		t.Fatalf("playlist IDs = %v", calledPlaylists)
	}
	if lib.called != 0 {
		t.Fatalf("librarian officer called %d times, want 0", lib.called)
	}
	if queue.captures[0].Status != "done" {
		t.Fatalf("status = %q, want done", queue.captures[0].Status)
	}
	if !strings.Contains(queue.captures[0].Error, "youtube playlist routed: 3 new videos") && queue.captures[0].Error != "" {
		t.Fatalf("unexpected queue note/error = %q", queue.captures[0].Error)
	}
	if got := queue.captures[0].VaultPath; got != filepath.Join(vaultDir, "brain", "youtube") {
		t.Fatalf("vault path = %q, want brain/youtube dir", got)
	}
}

func TestScoutKeepsTweetWithTextButEmptyBody(t *testing.T) {
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

	// Tweet capture: empty title/body/selected, but Tweet.Text is populated.
	cap := &Capture{
		Source: "x-extension",
		URL:    "https://x.com/i/status/1234567890",
		Tweet: &CaptureTweet{
			TweetID: "1234567890",
			Author:  "TestUser",
			Handle:  "@testuser",
			Text:    "This is an important tweet with real content.",
		},
	}
	if err := queue.Enqueue(cap); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	lib := &testLibrarianOfficer{}
	n, err := ProcessQueueWithOfficers(queue, state, vaultDir, 50, OfficerPipeline{
		Scout:     defaultScoutOfficer{model: "test"},
		Librarian: lib,
	})
	if err != nil {
		t.Fatalf("process queue: %v", err)
	}
	if n != 1 {
		t.Fatalf("written count = %d, want 1 (tweet should not be discarded)", n)
	}
	if lib.called != 1 {
		t.Fatalf("librarian called %d times, want 1", lib.called)
	}
	if queue.captures[0].Status == "discarded" {
		t.Fatalf("tweet with Tweet.Text should not be discarded")
	}
}

func TestYouTubeRoutingHelpers(t *testing.T) {
	cases := []struct {
		url          string
		wantVideo    bool
		wantPlaylist string
	}{
		{"https://www.youtube.com/watch?v=YpPcDHc3e9U", true, ""},
		{"https://youtu.be/YpPcDHc3e9U", true, ""},
		{"https://www.youtube.com/playlist?list=PLabc1234567890", false, "PLabc1234567890"},
		{"https://www.youtube.com/watch?v=YpPcDHc3e9U&list=PLabc1234567890", true, "PLabc1234567890"},
		{"https://example.com/watch?v=YpPcDHc3e9U", false, ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("route_%s", tc.url), func(t *testing.T) {
			if got := isYouTubeWatchURL(tc.url); got != tc.wantVideo {
				t.Fatalf("isYouTubeWatchURL(%q) = %v, want %v", tc.url, got, tc.wantVideo)
			}
			if got := extractYouTubePlaylistID(tc.url); got != tc.wantPlaylist {
				t.Fatalf("extractYouTubePlaylistID(%q) = %q, want %q", tc.url, got, tc.wantPlaylist)
			}
		})
	}
}
