# Claims and Safety

This document separates verified claims from aspirational ones. Every verified claim includes the evidence command or code reference.

## Verified Claims

### Queue dedup is deterministic
**Evidence:** `go/internal/wraith/queue.go:402` — `buildFingerprint()` computes SHA-256 of `source|tweetID|URL|title|body[:512]`. No randomness.
**Test:** `TestQueueEnqueueIngressDedup` in `queue_test.go` — identical captures produce same fingerprint, second is deduped.

### Near-duplicate detection uses Jaccard word similarity at threshold 0.82
**Evidence:** `go/internal/wraith/queue.go:360-380` — `findNearDuplicate()` calls `captureSimilarity()` which uses `jaccardWordSimilarity()` with threshold `>= 0.82`.
**Test:** `TestQueueEnqueueNearDuplicateFold` in `queue_test.go` — two captures with ~88% overlap are near-deduped.

### Scout discard skips Librarian filing
**Evidence:** `go/internal/wraith/consumer.go:53-56` — `assessment.Class == "discard"` branch sets status to "discarded" and does not call `officers.Librarian.File()`.
**Test:** `TestProcessQueueWithOfficersDiscardSkipsLibrarian` in `consumer_test.go` — librarian mock called 0 times for discard.

### Librarian write failure marks capture as failed (not lost)
**Evidence:** `go/internal/wraith/consumer.go` — write error sets `queue.SetStatus(cap.ID, "failed", err.Error())`, capture remains in queue.
**Test:** `TestAcceptanceFailurePathMarksFailed` in `acceptance_test.go` — simulated librarian failure → status "failed", error message preserved.

### Every processed capture gets a JSONL handoff record
**Evidence:** `go/internal/wraith/consumer.go` — `appendOfficerHandoff()` called for both keep and discard paths.
**Test:** `TestProcessQueueWithOfficersDiscardSkipsLibrarian` and `TestProcessQueueWithOfficersKeepFilesViaLibrarian` — both verify handoff file exists and contains correct record.

### WebSocket bridge listens only on localhost
**Evidence:** `go/internal/server/wraith.go` — binds to server's listener. Server binds to `127.0.0.1:{port}` by default.
**Verify:** `lsof -i :8781 -sTCP:LISTEN` — confirm bound to `127.0.0.1`, not `0.0.0.0`.

### State dedup prevents re-ingestion across runs
**Evidence:** `go/internal/wraith/consumer.go:41` — `state.Exists(source, externalID)` check before Scout assessment.
**Test:** `TestAcceptanceStateDuplicateGetsDeduped` in `acceptance_test.go` — pre-seeded state record causes dedup.

### Queue survives server restart
**Evidence:** `go/internal/wraith/queue.go:102-119` — `OpenQueue()` reads existing `wraith-queue.json` on startup.
**Verify:** Restart server, confirm `curl /wraith/status` shows same queue counts.

### Extension queues locally when server unreachable
**Evidence:** `apps/MODUSBridge/.../background.js:142-175` — `queueLocally()` stores up to 200 captures in `browser.storage.local`, `flushLocalQueue()` replays on reconnect.
**Verify:** Disconnect server, capture a page, reconnect — capture appears in queue.

### YouTube watch URLs route to dedicated transcript-aware ingest
**Evidence:** `go/internal/wraith/consumer.go:45-53` — `processDirectYouTubeCapture()` intercepts YouTube URLs before Scout assessment. `isYouTubeWatchURL()` fires first for `youtube.com/watch?v=` and `youtu.be/` URLs. `extractYouTubePlaylistID()` fires second for `youtube.com/playlist?list=` URLs.
**Test:** `TestProcessQueueRoutesYouTubeVideoDirectly`, `TestProcessQueueRoutesYouTubePlaylistDirectly`, `TestProcessQueuePrefersYouTubeVideoOverPlaylistContext`, `TestYouTubeRoutingHelpers` in `consumer_test.go`.

### Watch URLs with playlist context prefer single-video ingest
**Evidence:** `go/internal/wraith/consumer.go:132-147` — `isYouTubeWatchURL()` check fires before `extractYouTubePlaylistID()`. A URL like `youtube.com/watch?v=X&list=Y` matches the watch check first, routing to single-video ingest rather than triggering a full playlist walk.
**Test:** `TestProcessQueuePrefersYouTubeVideoOverPlaylistContext` in `consumer_test.go` — verifies video ingest is called, playlist ingest is not.

### YouTube ingest includes Librarian extraction
**Evidence:** `go/internal/wraith/ingest_yt.go:111-119` (playlist), `ingest_yt.go:431-439` (single-video) — `librarianExtract()` sends transcript to Gemma 4 26B on llama-server :8090 to produce structured sections (Summary, Key Ideas, Technical Details, Actionable Takeaways, Quotes, References).
**Verify:** Ingest a YouTube video with transcript available, check vault file for `## Librarian Extraction` section.

### Bridge origin allowlist admits browser-extension schemes
**Evidence:** `go/internal/server/server.go` — `isAllowedOrigin()` admits `safari-web-extension://`, `chrome-extension://`, `moz-extension://` in addition to localhost origins.
**Test:** `go test ./internal/server` passes after patch. Live verification: Safari extension connects without 403 handshake failure.

## Non-Claims (Explicitly Not Guaranteed)

### Content integrity
WRAITH does not guarantee that captured content matches the original page. JavaScript-rendered content, paywalled content, and dynamic pages may produce incomplete extractions. The content script extracts what is available in the DOM at `document_idle`.

### Real-time ingestion
Heartbeat cadences run every 2 hours (ingestion) and 4 hours (triage). Captures from the Safari extension are enqueued immediately but processed on the next cadence cycle. There is no streaming pipeline.

### Cross-browser support
Only Safari is supported via the MODUS Bridge extension. The WebSocket protocol is browser-agnostic, but no Chrome/Firefox extension exists.

### Triage accuracy
Triage classification (ADAPT/KEEP/MORE_INFO/DISCARD) depends on the Librarian LLM (Gemma 4 26B on llama-server :8090). If the LLM is unavailable, triage is skipped. Classification quality depends on model capability and prompt engineering.

### YouTube transcript availability
YouTube Librarian extraction depends on a transcript being available for the video. Videos without captions (auto-generated or manual) produce vault files with metadata and chapters but no extraction section. yt-dlp must be installed and accessible on `$PATH`.

### Cookie freshness
Safari cookie-based authentication (X, Reddit, Audible) depends on active Safari sessions. If cookies expire or Safari is not logged in, source fetches will fail silently (items remain in queue as "failed").

## Known Limitations

1. **Single WebSocket connection.** The bridge accepts one extension connection at a time. A new connection closes the previous one. (`go/internal/server/wraith.go:93-95`)
2. **No pagination on queue endpoint.** `/wraith/queue` returns up to 100 pending captures. Large backlogs require direct queue file inspection.
3. **Triage requires llama-server.** Without `llama-server` on `:8090`, triage classification is skipped entirely. Items remain unclassified.
4. **bird CLI required for X.** X/Twitter ingestion requires `@steipete/bird` npm package installed globally. Without it, X source is non-functional.
5. **Queue file grows unboundedly** without manual pruning. Use `Queue.Prune()` or the prune heartbeat to remove completed items.
6. **No encryption at rest.** Vault files and queue/state JSON are plain text. Filesystem permissions are the only access control.

## Roadmap Claims (Future, Not Implemented)

- **Chrome extension**: Porting the Safari extension to Chrome/Manifest v3. Not started.
- **Encrypted vault**: AES-256 encryption for vault files. Planned, no code written.
- **Streaming pipeline**: Real-time processing without heartbeat cadences. Under consideration.
- **Multi-user**: Separate vaults per user identity. Not designed.
