# Safari Extension and WebSocket Bridge

## Extension Overview

MODUS Bridge is a Safari Web Extension (`apps/MODUSBridge/`) that captures browser content and sends it to the MODUS daemon over a local WebSocket. It uses manifest v2 with a persistent background page to maintain the connection across tab changes.

The extension injects a content script (`content.js`) into every page. When a capture is triggered -- via context menu, keyboard shortcut, or sidepanel -- the background script messages the content script to extract the page, then forwards the result to the daemon.

## Message Types

All messages are JSON envelopes with a `type` field:

| Type | Direction | Purpose |
|------|-----------|---------|
| `hello` | Extension -> Server | Sent on connect. Payload: `{userAgent, extensionId}` |
| `hello_ack` | Server -> Extension | Confirms connection. Payload: `{server: "modus", version: "1.0"}` |
| `capture` | Extension -> Server | Single page capture. Payload contains URL, title, body, headings, links, images, tweet data |
| `capture_ack` | Server -> Extension | Confirms enqueue. Payload: `{ok: true, id: "..."}` or `{ok: false, error: "..."}` |
| `bookmark_batch` | Extension -> Server | Batch of captures (used by X bookmark sync). Payload: `{items: [...]}` |
| `batch_ack` | Server -> Extension | Confirms batch. Payload: `{ok: true, enqueued: N, total: M}` |
| `status` | Extension -> Server | Extension reporting its own status. Logged by server. |
| `command_result` | Extension -> Server | Response to a server-initiated command. Logged by server. |

The server can also push commands to the extension via `SendCommand(command, params)`, which generates an envelope with an `id` field (format: `cmd-{random hex}`).

## Connection Lifecycle

1. Background script calls `connect()` on startup
2. Before opening the WebSocket, `autoSelectDaemonUrl` probes candidate URLs via HTTP GET to `/wraith/status` (XHR, 1.2s timeout)
3. Candidate ports are tried in order: current `WS_URL`, then 8781, 8782, 8783, 8780
4. First port that responds with HTTP 2xx is selected
5. WebSocket opens to `ws://127.0.0.1:{port}/wraith/ws`
6. Extension sends `hello` message with user agent and extension ID
7. Server responds with `hello_ack`
8. Connection state is persisted to `ext.storage.local` and browser action title updated

### Auto-Reconnect

On WebSocket close or error, the extension schedules reconnection after a 3-second delay (`RECONNECT_DELAY_MS = 3000`). The reconnect cycle re-runs daemon discovery, so if the server moved ports, the extension will find it.

### Local Queue Fallback

If the WebSocket is not connected when a capture is triggered, the payload is stored in `ext.storage.local` under the key `pendingCaptures`. The local queue is capped at **200 items** (oldest items are dropped when the cap is exceeded). When the WebSocket reconnects, `flushLocalQueue()` sends all pending captures and clears the local store.

## Content Extraction

The content script (`content.js`) extracts structured data from the current page:

- **Body text**: Cloned DOM with noise elements removed (script, style, nav, footer, header, aside, iframe, svg, canvas). Capped at **50,000 characters**.
- **Headings**: Up to **100 headings** (h1-h6), each capped at 500 characters.
- **Links**: Up to **300 links** with text (capped at 200 chars) and href. Filters out javascript: and mailto: links.
- **Images**: Up to **50 images** with minimum dimensions of **100x100 pixels**. Records src, alt, width, height.
- **Metadata**: og:description, author, og:site_name, article:published_time, og:image via meta tag extraction.

### X/Twitter-Specific Extraction

On `x.com` and `twitter.com`, the content script detects tweet pages and the bookmarks page:

- **Single tweet** (`/username/status/ID`): Extracts author, handle, text, timestamp, engagement metrics, media URLs, quoted tweet content, and up to 20 thread replies from `article[data-testid="tweet"]` elements.
- **Bookmarks page** (`/i/bookmarks`): Extracts all visible bookmarked tweets as a batch, including tweet IDs parsed from status links.

The content script also responds to `extract_bookmarks` and `scroll_down` messages, enabling programmatic bookmark harvesting from the background script.

## Server-Side Bridge

The `WraithBridge` struct in `go/internal/server/wraith.go` manages the WebSocket connection and queue. Key behaviors:

- Only one extension connection is active at a time. New connections close the previous one.
- `BridgeStatus` tracks connection state, extension ID, user agent, and last message timestamp.
- HTTP endpoints expose bridge status (`/wraith/status`), queue contents (`/wraith/queue`), and source stats (`/wraith/sources`).
- The origin allowlist admits browser-extension schemes (`safari-web-extension://`, `chrome-extension://`, `moz-extension://`) in addition to `http://127.0.0.1` origins. This was required to resolve WebSocket 403 handshake failures when Safari extensions connect with their extension-scheme origin header.

## YouTube Page Behavior

When the extension captures a YouTube page, the queue consumer intercepts the URL before the generic Scout→Librarian pipeline:

- **Watch page** (`youtube.com/watch?v=` or `youtu.be/`): Routed to `IngestYouTubeVideo()` — yt-dlp fetches transcript, metadata, and chapters. Librarian extracts structured knowledge sections if a transcript is available. Filed to `brain/youtube/`.
- **Playlist page** (`youtube.com/playlist?list=`): Routed to `IngestYouTube()` — walks all videos in the playlist, applies per-video transcript extraction. Filed to `brain/youtube/`.
- **Watch page with playlist context** (`youtube.com/watch?v=...&list=...`): Routed as single-video, not full playlist. The watch URL check fires before the playlist check in `processDirectYouTubeCapture()`.

This routing is transparent to the extension — it sends a normal `capture` message. The consumer-side URL check handles the rest.
