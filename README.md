# WRAITH

Browser intelligence capture for personal knowledge vaults.

WRAITH watches what you save — bookmarks, selections, tweets, starred repos — and files them as searchable markdown in your vault. A Safari extension captures pages over WebSocket. Five ingestion sources pull from X, GitHub, Reddit, YouTube, and Audible. A two-officer pipeline (Scout → Librarian) classifies and files every capture with an auditable handoff trail.

Everything stays local. No cloud sync, no telemetry, no third-party storage.

```
Browser ──WebSocket──► Bridge ──► Queue ──► Scout ──► Librarian ──► Vault
                                    │         │                        │
                                    │         └─ YouTube URL? ─► yt-dlp + Librarian extract ─► brain/youtube/
                                    └── dedup (fingerprint + Jaccard) ─┘
```

## End-to-End Flow

1. **Capture.** Safari extension extracts page content (title, body, headings, links, images, tweets) and sends a JSON envelope over WebSocket to `ws://127.0.0.1:{port}/wraith/ws`.
2. **Bridge.** The Go server accepts the WebSocket connection, parses the envelope, and enqueues a `Capture` struct into `wraith-queue.json`.
3. **Dedup.** Two-pass ingress deduplication:
   - **Fingerprint**: SHA-256 of `source|tweetID|URL|title|body[:512]`. Exact match → fold into canonical capture.
   - **Near-duplicate**: Jaccard word similarity on normalized URL + title + body (threshold ≥ 0.82). Soft fold with similarity score recorded.
4. **YouTube routing.** Before Scout assessment, the consumer checks for YouTube URLs. Watch URLs (`youtube.com/watch?v=`, `youtu.be/`) route to single-video ingest via yt-dlp (transcript + metadata + Librarian extraction). Playlist URLs (`youtube.com/playlist?list=`) route to playlist ingest. Watch URLs that also contain a `list=` parameter still route as single-video (not full playlist). Both paths write to `brain/youtube/`.
5. **Scout.** Classifies non-YouTube captures as `keep`, `discard`, or `mission_candidate`. Heuristic rules: GitHub URLs or "release"/"CVE-" in title → mission candidate. Empty body → discard.
6. **Librarian.** Writes `keep` and `mission_candidate` captures to vault as markdown with YAML frontmatter. Files under `brain/{source}/YYYY-MM-DD-{slug}.md`. Computes SHA-256 checksum of written content.
7. **Audit.** Every Scout→Librarian handoff is appended to `wraith-officer-handoffs.jsonl` as a single JSONL line with capture ID, fingerprint, assessment, and filing receipt.
8. **State.** Ingested items recorded in `wraith-state.json` keyed by `source|externalID` for cross-run dedup.

## Ingestion Sources

| Source | Method | Auth | Dedup Key |
|--------|--------|------|-----------|
| Safari extension | WebSocket capture | None (localhost) | fingerprint hash |
| X/Twitter | `bird` CLI → Safari cookies | Safari cookie jar | `x-bookmarks\|tweet_id` |
| GitHub stars | REST API | `GITHUB_TOKEN` env | `github-stars\|full_name` |
| Reddit saved | Reddit JSON API | Safari cookie jar | `reddit-saved\|post_id` |
| YouTube | yt-dlp (transcripts, metadata) | None (yt-dlp handles auth) | `youtube-videos\|video_id` |
| Audible | Private API | Safari cookie jar | `audible-highlights\|asin` |

## Runtime

WRAITH runs as part of the MODUS server. Relevant endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/wraith/ws` | GET (upgrade) | WebSocket for Safari extension |
| `/wraith/status` | GET | Queue stats, bridge connection, handoff counts |
| `/wraith/queue` | GET | Pending captures (limit 100) |
| `/wraith/sources` | GET | Ingestion source statistics |

Default port: 8781. The Safari extension probes ports 8781, 8782, 8783, 8780 for daemon discovery. The WebSocket bridge accepts connections from browser-extension origins (`safari-web-extension://`, `chrome-extension://`, `moz-extension://`) in addition to localhost.

## Quickstart

```bash
# Build the bridge
go build -o wraith-bridge ./cmd/wraith-bridge/

# Start the bridge (listens on 127.0.0.1:8781)
./wraith-bridge

# Check WRAITH status
curl -s http://127.0.0.1:8781/wraith/status | jq .

# Run tests
go test ./internal/... -v
```

## Queue State Machine

Every capture passes through a deterministic state machine:

```
captured → queued → processing → filed
                  → deduped
                  → discarded (scout: discard)
                  → failed (librarian write error)
                  → triaged → filed
                            → mission_candidate → filed
```

All transitions are recorded in `ingest_history[]` on the Capture struct with timestamps and notes.

## Data Files

| File | Format | Purpose |
|------|--------|---------|
| `wraith-queue.json` | JSON array | Durable capture queue with full ingest history |
| `wraith-state.json` | JSON array | Cross-run dedup ledger (source\|externalID → record) |
| `wraith-officer-handoffs.jsonl` | JSONL | Audit trail of Scout→Librarian handoffs |

## What WRAITH Is Not

- **Not a web scraper.** WRAITH captures content you explicitly save or bookmark. It does not crawl, spider, or fetch pages you haven't interacted with.
- **Not a sync service.** There is no cloud component. Data stays on your machine in plain markdown files.
- **Not an AI summarizer.** The Scout and Librarian officers classify and file. The Librarian extracts structured knowledge sections from YouTube transcripts (Summary, Key Ideas, Technical Details, Actionable Takeaways, Quotes, References) via Gemma 4 26B, but does not rewrite or editorialize other captured content.
- **Not a browser extension platform.** The Safari extension is purpose-built for MODUS capture. It does not inject UI, modify pages, or track browsing.

## Privacy Posture

- All data stored locally as plain markdown files and JSON.
- No outbound network calls except to configured source APIs (GitHub, YouTube via yt-dlp, X via bird CLI, Reddit, Audible). Each source is opt-in and runs only when triggered by heartbeat or manual invocation.
- Safari cookie access is read-only, used only for authenticated source fetches (X, Reddit, Audible). Cookies are consumed in-memory and not persisted separately.
- The WebSocket bridge listens only on `127.0.0.1` — not exposed to the network.
- No telemetry, analytics, or crash reporting.

## Safety Posture

- Dedup is deterministic (SHA-256 fingerprint + Jaccard threshold). No probabilistic matching.
- Queue persistence: captures survive server restarts via `wraith-queue.json`.
- Failure isolation: a Librarian write failure marks the capture `failed` but does not crash the pipeline or lose the capture.
- Audit trail: every processed capture has a JSONL handoff record regardless of outcome.
- The extension queues up to 200 captures locally if the server is unreachable, flushing on reconnect.

## Project Structure

```
cmd/wraith-bridge/          Standalone bridge binary
internal/wraith/            Core package (queue, state, consumer, officers, ingestion)
internal/server/            WebSocket bridge
internal/markdown/          YAML frontmatter markdown parser
internal/moduscfg/          Officer configuration
extension/                  Safari extension (background.js, content.js, manifest.json)
assets/                     SVG diagrams
docs/                       Architecture, operations, testing docs
fixtures/                   Deterministic test payloads
scripts/                    Operator scripts
```

## License

MIT. See [LICENSE](LICENSE).
