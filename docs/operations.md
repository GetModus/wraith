# Operations

## Server Startup

WRAITH runs as part of the MODUS server. The WebSocket bridge is initialized when the HTTP server starts, listening on the `/wraith/ws` endpoint. The default port is 8781, but the extension probes 8780-8783.

HTTP endpoints available once the server is running:

- `GET /wraith/status` -- Bridge connection state, queue stats, handoff stats
- `GET /wraith/queue` -- Current queue contents (up to 100 pending items)
- `GET /wraith/sources` -- Ingestion source stats from wraith state

## Checking Status

Query the status endpoint to see bridge connectivity, queue depth, and handoff counts:

```bash
curl http://127.0.0.1:8781/wraith/status
```

The response includes:
- `bridge.connected` -- Whether the Safari extension is connected
- `queue.queued` / `queue.done` / `queue.deduped` -- Queue item counts by status
- `handoffs.total` / `handoffs.by_class` -- Officer handoff breakdown

## Source Configuration

### Environment Variables

| Variable | Purpose | Required |
|----------|---------|----------|
| `GITHUB_TOKEN` | GitHub API authentication (avoids rate limits) | Recommended |
| `GITHUB_USER` | GitHub username for starred repos ingestion | For GitHub source |
| `YOUTUBE_API_KEY` | YouTube Data API key | Not used (yt-dlp is primary) |
| `MODUS_LIBRARIAN_URL` | Override librarian endpoint (default: `http://127.0.0.1:8090/v1`) | No |
| `WRAITH_REDDIT_DETAIL_COUNT` | Max Reddit posts to capture detail for | No (default: min(maxItems, 25)) |
| `WRAITH_REDDIT_MAX_COMMENTS` | Max comments to capture per Reddit post | No (default: 10) |

### Source-Specific Requirements

- **X (Twitter)**: Requires `bird` CLI (`npm install -g @steipete/bird`). Uses Safari cookies for auth.
- **GitHub**: Works without a token but will hit rate limits. Token set via `GITHUB_TOKEN`.
- **Reddit**: Requires an active `token_v2` cookie from a logged-in Safari session on reddit.com.
- **YouTube**: Requires `yt-dlp` at `/opt/homebrew/bin/yt-dlp`. Falls back to RSS feeds (metadata only, no transcripts) if unavailable.
- **Audible**: Reads `.md` and `.txt` files from `~/modus/data/audible/`. No external auth needed.

## Heartbeat Cadences

WRAITH ingestion is triggered by the heartbeat system (`scripts/heartbeat.sh`):

- **Ingestion**: Every 2 hours. Runs `IngestX`, `IngestGitHub`, `IngestReddit`, `IngestYouTube`, `IngestAudible`.
- **Triage**: Every 4 hours. Runs `Triage` on pending items in state, classifying via Gemma 4 26B.

## Pruning Old Captures

The queue grows as captures accumulate. Completed items (done, failed, deduped, discarded) can be pruned:

```go
pruned := queue.Prune(7 * 24 * time.Hour) // Remove completed items older than 7 days
```

Queued and processing items are never pruned regardless of age.

## Monitoring the Handoff Ledger

The handoff ledger at `~/modus/data/wraith-officer-handoffs.jsonl` is an append-only log. Each line is a JSON object recording the scout assessment and librarian receipt for one capture.

The `/wraith/status` endpoint includes parsed handoff stats: total records, count with librarian receipt, breakdown by scout class, last capture ID and timestamp, and parse error count.

To inspect manually:

```bash
# Count by scout class
cat ~/modus/data/wraith-officer-handoffs.jsonl | jq -r '.scout.class' | sort | uniq -c

# Recent filings
tail -5 ~/modus/data/wraith-officer-handoffs.jsonl | jq '{id: .capture_id, class: .scout.class, path: .librarian.vault_path}'
```

## iMessage Notifications

After triage runs, `NotifyIngestion` sends a summary via iMessage using AppleScript. The message includes ADAPT items with reasons, up to 5 KEEP titles, and DISCARD count. Messages are capped at 800 characters.

The notification is sent to the configured phone number in `notify.go`. This requires macOS with Messages.app configured for iMessage.
