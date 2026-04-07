# Troubleshooting

## Extension Not Connecting

**Symptom**: Bridge shows `connected: false` in `/wraith/status`. Sidepanel shows disconnected.

**Checks**:
1. Verify the MODUS server is running and the WebSocket endpoint is active. `curl http://127.0.0.1:8781/wraith/status` should return JSON.
2. The extension probes ports 8780-8783 in sequence. If the server is on a non-default port, the extension will find it automatically -- but only within that range.
3. In Safari, check that the MODUS Bridge extension is enabled (Safari > Settings > Extensions).
4. The extension uses a persistent background page (manifest v2). If Safari killed it, toggling the extension off and on restarts it.
5. Check the Safari Web Inspector console for the extension background page. Look for `[MODUS Bridge]` log lines.

## Queue Growing Unbounded

**Symptom**: `queue.total` in `/wraith/status` keeps climbing, even for completed items.

**Cause**: Completed captures (done, deduped, discarded, failed) are never automatically removed. They stay for audit purposes.

**Fix**: Call `queue.Prune(duration)` to remove completed items older than the specified duration. A 7-day window is reasonable for most use cases.

## Dedup Not Catching Duplicates

**Fingerprint dedup misses**: The fingerprint includes `source|tweetID|URL|title|body[:512]`. If any of these fields differ (e.g., the page title changed between captures, or the body text shifted), the fingerprint will not match. This is by design -- the near-dedup layer handles these cases.

**Near-dedup misses**: Near-dedup requires the normalized URLs to match exactly (after stripping protocol, www, trailing slash). If the URLs differ beyond that normalization, near-dedup will not fire. The Jaccard threshold is 0.82 -- items below that similarity are considered distinct. Tokens shorter than 3 characters are discarded before comparison.

**State dedup misses**: The consumer checks `State.Exists(source, externalID)`. The source string is derived from the capture (e.g., `x-extension` for tweets, `reddit-extension` for Reddit URLs, `extension-{source}` for others). If the same URL was ingested via a batch source (e.g., `x-bookmarks`) and then captured by the extension (e.g., `x-extension`), the source strings differ and state dedup will not match. Queue fingerprint dedup is the first line of defense here.

## Source Auth Failures

**Reddit**: `token_v2` cookies expire. Error message: "reddit auth failed (401) -- token_v2 may be expired". Fix: log into reddit.com in Safari, then retry. The cookie is read directly from Safari's binary cookie store at `~/Library/Containers/com.apple.Safari/Data/Library/Cookies/Cookies.binarycookies`.

**X (Twitter)**: The `bird` CLI uses Safari cookies for auth. If bird returns exit code 1 with auth errors, re-authenticate in Safari by visiting x.com. Run `bird bookmarks --cookie-source safari --json --count 1` to test.

**GitHub**: Without `GITHUB_TOKEN`, the API allows 60 requests/hour (unauthenticated). Error: "github rate limited". Set the token or reduce `maxItems`.

**YouTube**: `yt-dlp` handles its own auth. Rate limiting (HTTP 429) will cause transcript download to fail silently -- metadata is still captured. If `yt-dlp` is missing entirely, the system falls back to RSS feeds.

## Triage Not Running

**Symptom**: Items stay at `triage: "pending"` indefinitely.

**Checks**:
1. The triage system requires Gemma 4 26B running on llama-server at `http://127.0.0.1:8090`. Check: `curl http://127.0.0.1:8090/v1/models`.
2. If the endpoint is unavailable, `Triage` returns an error and logs "librarian unavailable on :8090 -- skipping". No items are classified.
3. Override the endpoint with `MODUS_LIBRARIAN_URL` if the model server is on a different port.
4. Triage runs on items with `triage: "pending"` in wraith state. If state has no pending items, triage has nothing to do.
