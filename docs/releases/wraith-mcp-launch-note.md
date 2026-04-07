# WRAITH v0.2.0 — MCP Control Surface

**Date:** 2026-04-07
**Repo:** [github.com/GetModus/wraith](https://github.com/GetModus/wraith)

---

## What shipped

WRAITH now has two ways in.

The original path — a Safari extension sending captures over WebSocket to a local bridge — still works exactly as before. What's new is a standalone MCP server that exposes the same pipeline as tool calls. Any MCP-compatible harness can now enqueue captures, inspect queue state, and trigger processing without a browser.

Both paths share the same queue, the same dedup logic, the same Scout and Librarian officers, and the same vault. The MCP server is not a reimplementation. It calls the same Go functions the bridge uses.

## Why this matters

WRAITH started as a browser capture tool. The MCP surface turns it into a general-purpose intake system. A coding agent can submit content it finds relevant. A cron job can push RSS items. A custom script can feed in API responses. All of it goes through the same deterministic pipeline — fingerprint dedup, near-duplicate detection, officer triage, vault filing — and ends up as searchable markdown.

This is the pattern we're building toward across MODUS: standalone modules that work on their own but compose when you put them together. WRAITH captures. [modus-memory](https://github.com/GetModus/modus-memory) retrieves. Each is useful alone. Together they close the loop from "something you read" to "something your AI remembers."

## What's usable today

- **Browser capture** via Safari extension + WebSocket bridge (`wraith-bridge`)
- **MCP capture** via standalone MCP server (`wraith-mcp`) — works with Claude Code, Cursor, Windsurf, or any MCP client
- **Five ingestion sources:** X/Twitter bookmarks, GitHub stars, Reddit saved, YouTube transcripts, Audible highlights
- **Two-officer pipeline:** Scout classifies, Librarian files to vault with YAML frontmatter
- **Deterministic dedup:** SHA-256 fingerprint + Jaccard word similarity (threshold ≥ 0.82)
- **Audit trail:** every capture logged to JSONL regardless of outcome

## MCP tools

| Tool | What it does |
|------|-------------|
| `wraith_status` | Queue stats, source-level ingest counts, data/vault directory info |
| `wraith_queue` | List pending captures |
| `wraith_capture` | Submit a capture directly (source, URL, title, body, tweet data) |
| `wraith_process` | Run queued captures through YouTube routing → Scout → Librarian |
| `wraith_sources` | Source-level ingest statistics |

## Setup (MCP)

```json
{
  "mcpServers": {
    "wraith": {
      "command": "wraith-mcp",
      "env": {
        "MODUS_VAULT_DIR": "~/vault",
        "MODUS_DATA_DIR": "~/vault/data"
      }
    }
  }
}
```

## What's next

WRAITH is one module in the MODUS architecture. More modules will follow as standalone repos. The long-term destination is MODUS OS — the place where all the modules work best together — but each module ships independently and works on its own first.

## Technical details

- Go 1.22, single binary (~6MB each), no CGo, no cloud dependency
- Queue persists to disk, survives restarts
- WebSocket bridge on `127.0.0.1` only — never exposed to network
- MIT licensed
