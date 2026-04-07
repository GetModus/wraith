# Architecture

## Package Layout

WRAITH has two control surfaces feeding one shared pipeline:

| Component | Path | Role |
|-----------|------|------|
| Core package | `internal/wraith/` | Queue, state, officers, ingestion, triage, cookies, Safari automation |
| WebSocket bridge | `internal/server/wraith.go` | Receives captures from the Safari extension over WebSocket |
| MCP tools | `internal/mcp/wraith.go` | Exposes queue, capture, process, status, sources as MCP tool calls |
| Bridge binary | `cmd/wraith-bridge/main.go` | Standalone WebSocket server for browser capture |
| MCP binary | `cmd/wraith-mcp/main.go` | Standalone MCP server for harness capture |
| Safari extension | `extension/` | Content extraction and real-time capture |

No external database — all persistence is JSON files on disk.

## Two Control Surfaces, One Pipeline

The bridge and MCP server are independent entry points into the same queue and officer pipeline. They can run simultaneously.

```
[Safari Extension] --WebSocket--> [WraithBridge] --Enqueue--> [Queue]
[MCP Client]       --wraith_capture--> [MCP Server] --Enqueue--> [Queue]  (same queue)
[bird CLI / GitHub API / Reddit API / yt-dlp / Audible] --Ingest*--> [State] + [Vault]
[Queue] --ProcessQueue--> [Scout] --assess--> [Librarian] --file--> [Vault .md files]
[State.PendingTriage] --Triage--> [Gemma 4 26B on :8090] --classify--> [State triage update] + [RouteTriagedFile]
```

The MCP path provides five tools:
- `wraith_status` — queue stats and source-level ingest counts
- `wraith_queue` — inspect pending captures
- `wraith_capture` — enqueue a capture directly (source, URL, title, body, tweet payload)
- `wraith_process` — run queued captures through YouTube routing, Scout, and Librarian
- `wraith_sources` — source-level ingest statistics

The MCP server does not duplicate the pipeline. It calls the same `wraith.OpenQueue`, `wraith.Enqueue`, `wraith.ProcessQueue`, and `wraith.OpenState` functions as the bridge consumer. Queue persistence, dedup, and officer handoffs are identical regardless of entry point.

### Real-time path (extension captures)

1. Safari extension extracts page content (content.js)
2. Extension sends `capture` message over WebSocket to bridge
3. `WraithBridge.handleMessage` creates a `Capture` struct and calls `Queue.Enqueue`
4. Enqueue computes fingerprint, checks exact and near-duplicate, persists to `wraith-queue.json`
5. `ProcessQueue` picks up pending items
6. **YouTube URL check** — before Scout assessment, `processDirectYouTubeCapture()` intercepts YouTube URLs:
   - Watch URLs (`youtube.com/watch?v=`, `youtu.be/`) → `IngestYouTubeVideo()` (yt-dlp transcript + metadata + Librarian extraction) → `brain/youtube/`
   - Playlist URLs (`youtube.com/playlist?list=`) → `IngestYouTube()` (playlist walk + per-video transcript + Librarian extraction) → `brain/youtube/`
   - Watch URLs with `list=` parameter → single-video ingest (watch check fires before playlist check)
   - Non-YouTube URLs continue to step 7
7. Scout classifies remaining captures, files via Librarian
8. Librarian writes markdown to `brain/{source}/YYYY-MM-DD-{slug}.md`
9. State records the item for future dedup
10. Handoff record appended to `wraith-officer-handoffs.jsonl`

### Batch path (ingestion sources)

Each `Ingest*` function (IngestX, IngestGitHub, IngestReddit, IngestYouTube, IngestAudible) follows the same pattern:

1. Fetch items from the source API
2. Check `State.Exists(source, externalID)` for dedup
3. Build frontmatter and markdown body
4. Write directly to vault (these bypass the queue and officer pipeline)
5. Record in state

**YouTube ingest** has additional steps: yt-dlp fetches video metadata, chapters, and transcript. If a transcript is available, `librarianExtract()` sends it to Gemma 4 26B on llama-server :8090 to produce structured sections (Summary, Key Ideas, Technical Details, Actionable Takeaways, Quotes, References). Playlist ingest walks each video in the playlist and applies the same per-video extraction. Both paths write to `brain/youtube/`.

YouTube captures arriving via the Safari extension queue also use this dedicated path — they are intercepted by `processDirectYouTubeCapture()` in the consumer before reaching the generic Scout→Librarian pipeline.

### Triage path

The `Triage` function operates on items already in state with `triage: "pending"`:

1. Read vault file content for each pending item
2. Batch-classify via Gemma 4 26B on llama-server `:8090` (OpenAI-compatible API)
3. ADAPT items go through a dialectic challenge loop (up to 3 rounds of skeptic vs defender)
4. Update state triage classification
5. `RouteTriagedFile` moves vault files to `brain/keep/`, `brain/discard/`, or `brain/pending/` based on classification. ADAPT items stay in place.

## Persistence Layer

**wraith-queue.json** -- Array of `Capture` structs. Each capture carries its own `IngestHistory` (state transition ledger). File is rewritten on every mutation. Protected by `sync.RWMutex`.

**wraith-state.json** -- Array of `IngestionRecord` structs, keyed in memory as `source|external_id`. Tracks every item ever ingested for dedup and triage status. Rewritten on every mutation.

**wraith-officer-handoffs.jsonl** -- Append-only JSONL log. One line per officer pipeline execution, recording scout assessment and librarian receipt (if filed). Serves as the audit trail.

All three files live in the data directory (default `~/modus/data/`).

## Officer Pipeline

The pipeline is defined by two interfaces in `officer_pipeline.go`:

```go
type ScoutOfficer interface {
    Assess(c *Capture) ScoutAssessment
}

type LibrarianOfficer interface {
    File(vaultDir string, cap *Capture, frontmatter map[string]interface{}, body string) (FilingReceipt, error)
}
```

The default Scout classifies GitHub URLs and security/release content as `mission_candidate`, empty payloads as `discard`, and everything else as `keep`. The default Librarian writes markdown files using the `markdown.Write` helper and returns a `FilingReceipt` with a SHA-256 checksum of the body.

## Dedup Strategy

Two-layer dedup at queue ingress (see `queue-and-dedup.md` for details):

1. **Fingerprint dedup** -- SHA-256 of `source|tweetID|URL|title|body[:512]`. Exact match.
2. **Near-duplicate dedup** -- Jaccard word similarity on normalized URL + title + body[:1024]. Threshold: 0.82.

A third layer operates at processing time: the consumer checks `State.Exists` before filing, catching items ingested by batch sources that were also captured by the extension.

## Safari Extension Architecture

The extension uses **manifest v2** with a **persistent background page** (`background.html` loads `background.js`). This is required for maintaining the WebSocket connection to the MODUS daemon.

- **background.js** (~722 LOC) -- WebSocket lifecycle, context menu setup, capture dispatch, local queue fallback, daemon auto-discovery, X bookmark sync
- **content.js** (~337 LOC) -- DOM extraction (headings, links, images, body text), X/Twitter-specific tweet parsing, message handler for `extract_page`/`ping`/`extract_bookmarks`/`scroll_down`
- **sidepanel.js** (~130 LOC) -- UI for the extension side panel
- **manifest.json** -- Permissions for ports 8780-8783, `<all_urls>` content script injection

The extension connects to the daemon via WebSocket at `/wraith/ws`. If the primary port (8781) is unavailable, it probes 8781, 8782, 8783, 8780 in sequence. Connection state is persisted to `ext.storage.local` and the browser action title reflects connectivity.

The server-side origin allowlist admits browser-extension schemes (`safari-web-extension://`, `chrome-extension://`, `moz-extension://`) in addition to localhost origins, resolving WebSocket 403 handshake failures from Safari extensions.
