<p align="center">
  <strong>WRAITH</strong><br>
  Browser intelligence capture for personal knowledge vaults
</p>

---

WRAITH watches what you save — bookmarks, selections, tweets, starred repos — and files them as searchable markdown in your vault.

A Safari extension captures pages over WebSocket. An MCP server lets any AI harness enqueue captures programmatically. Five ingestion sources pull from X, GitHub, Reddit, YouTube, and Audible. Everything stays local. No cloud sync, no telemetry, no third-party storage.

## Architecture

```
wraith/
  bridge.py       WebSocket bridge (Safari extension → queue)
  core.py         Queue, state, dedup, pipeline
  routes.py       HTTP/WS route handlers
  ghost.py        Source ingestion (X, GitHub, Reddit, YouTube, Audible)
  cookies.py      Safari cookie reader for authenticated sources
  extension/      Safari extension (background.js, content.js, manifest.json)
```

## Quickstart

```bash
pip install -r requirements.txt

# Option A: Browser capture (Safari extension → WebSocket)
python bridge.py

# Option B: MCP capture (any harness → stdio)
python -m wraith.mcp
```

## Ingestion Sources

| Source | Method | Auth |
|--------|--------|------|
| Safari extension | WebSocket | None (localhost) |
| X/Twitter | Safari cookies | Cookie jar |
| GitHub stars | REST API | `GITHUB_TOKEN` |
| Reddit saved | Reddit JSON | Safari cookie jar |
| YouTube | yt-dlp | None |
| Audible | Private API | Safari cookie jar |

## Privacy

- All data stored locally as plain markdown and JSON
- WebSocket bridge listens only on `127.0.0.1`
- No telemetry, analytics, or crash reporting
- Cookie access is read-only, in-memory only

## License

MIT
