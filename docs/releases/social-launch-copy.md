# WRAITH Launch Copy

## GitHub Repo Description

Browser intelligence capture for personal knowledge vaults. Safari extension + MCP server. Local-first, plain markdown, deterministic dedup.

---

## GitHub Release Blurb (v0.2.0)

WRAITH v0.2.0 adds an MCP control surface. The same pipeline that captures browser content via WebSocket is now accessible as MCP tool calls — `wraith_capture`, `wraith_process`, `wraith_status`, `wraith_queue`, `wraith_sources`. Any MCP-compatible harness (Claude Code, Cursor, Windsurf) can enqueue captures and trigger processing without a browser.

Both paths share one queue, one dedup layer, one officer pipeline, one vault. The MCP server is not a second implementation — it calls the same Go functions.

Pairs with [modus-memory](https://github.com/GetModus/modus-memory) for capture-to-retrieval: WRAITH files content, modus-memory indexes it, your AI searches it.

---

## X / Twitter — Main Post

WRAITH is live. Open-source browser capture that files what you save as searchable markdown.

Now with two ways in: Safari extension over WebSocket, or MCP tools from any AI harness.

Same queue, same dedup, same pipeline. Local-only, plain files.

github.com/GetModus/wraith

---

## X / Twitter — Thread (5 posts)

**1/** Releasing WRAITH — browser intelligence capture for personal knowledge vaults.

It watches what you bookmark, save, and star. Files everything as markdown with YAML frontmatter. Deterministic dedup. Full audit trail. Local-only.

github.com/GetModus/wraith

**2/** Two control surfaces, one pipeline:

- Safari extension → WebSocket → bridge
- Any MCP client → wraith-mcp → same queue

Claude Code, Cursor, Windsurf, or your own agent can now submit captures and trigger processing. No browser required for the MCP path.

**3/** Five ingestion sources beyond browser capture:

- X/Twitter bookmarks (bird CLI + Safari cookies)
- GitHub starred repos (REST API)
- Reddit saved posts (JSON API)
- YouTube transcripts (yt-dlp + Librarian extraction)
- Audible highlights

All local. Each source is opt-in.

**4/** The pipeline is auditable and deterministic:

- SHA-256 fingerprint dedup (exact match)
- Jaccard word similarity dedup (threshold 0.82)
- Scout officer triages: keep / discard / mission_candidate
- Librarian files to vault with SHA-256 checksum
- Every handoff logged to JSONL

**5/** WRAITH is one module in the MODUS architecture.

Pair it with modus-memory: WRAITH captures → vault → modus-memory indexes → AI searches.

Two standalone repos, one knowledge system. More modules shipping soon.

github.com/GetModus/wraith
github.com/GetModus/modus-memory

---

## Discord Announcement

**WRAITH is live** — github.com/GetModus/wraith

Browser intelligence capture that files what you save as searchable markdown in your vault.

**What it does:**
- Safari extension captures pages, tweets, selections over WebSocket
- Five background sources: X bookmarks, GitHub stars, Reddit saved, YouTube transcripts, Audible highlights
- Scout → Librarian pipeline classifies and files with audit trail
- SHA-256 + Jaccard dedup — no duplicates, deterministic

**New in v0.2.0: MCP support**
A standalone `wraith-mcp` binary exposes the pipeline as MCP tool calls. Claude Code, Cursor, Windsurf, or any MCP client can enqueue captures and trigger processing. No browser needed.

5 MCP tools: `wraith_status`, `wraith_queue`, `wraith_capture`, `wraith_process`, `wraith_sources`

**Pairs with modus-memory** for the full capture-to-retrieval loop. WRAITH writes, modus-memory indexes, your AI searches.

Local-only. Plain markdown. MIT licensed. ~6MB binary.

---

## Website / Product Blurb (short)

**WRAITH** captures what you read online and files it as searchable markdown. A Safari extension sends pages over WebSocket. An MCP server lets any AI harness submit content programmatically. Five ingestion sources pull from X, GitHub, Reddit, YouTube, and Audible. Everything stays local as plain files. Pairs with modus-memory for AI-searchable knowledge.

---

## Technical / Builder Version

**WRAITH** — local-first capture pipeline for browser content and structured ingestion sources.

Two entry points into one pipeline: `wraith-bridge` (WebSocket server for Safari extension, HTTP status endpoints on 127.0.0.1:8781) and `wraith-mcp` (MCP server over stdio with 5 tools). Both call the same `wraith.OpenQueue`, `wraith.Enqueue`, `wraith.ProcessQueue` functions. Queue persistence is JSON on disk. Dedup is SHA-256 fingerprint + Jaccard word similarity at 0.82 threshold. Officer pipeline: Scout (heuristic triage) → Librarian (markdown filing with YAML frontmatter + SHA-256 checksum). Every processed capture gets a JSONL handoff record.

YouTube URLs are intercepted before Scout and routed to yt-dlp for transcript extraction + Librarian knowledge section generation (Summary, Key Ideas, Technical Details, Actionable Takeaways, Quotes, References).

Go 1.22, ~6MB binary, no CGo, two external deps (gorilla/websocket, gopkg.in/yaml.v3). MIT licensed.

Standalone module in the MODUS architecture. Works independently. Composes with modus-memory for capture → index → retrieval.

github.com/GetModus/wraith
