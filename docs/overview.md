# WRAITH: Browser Intelligence Capture

WRAITH is a Go-based module that captures content from the browser and files it as structured markdown in a personal vault. It exists because bookmarks rot, tabs accumulate, and the things you save are never the things you find again.

## Design Philosophy

**Local-first.** All data stays on disk. Queue state is a JSON file. Vault output is plain markdown with YAML frontmatter. No cloud services, no databases beyond what the filesystem provides. Safari cookies are read directly from the binary cookie store -- no browser extension APIs needed for auth.

**Auditable.** Every capture has a fingerprint. Every state transition is recorded in an ingest history ledger on the capture itself. Every officer decision (scout assessment, librarian filing) is logged to a JSONL handoff file. You can reconstruct what happened to any item.

**No rewriting of content.** WRAITH captures what the author wrote. Body text is truncated but never summarized or rephrased at capture time. Triage classification happens as a separate phase against the filed content. The vault file is the source of truth.

**Officer pipeline.** Captures pass through a Scout (classify: keep, discard, or mission_candidate) and a Librarian (write to vault with frontmatter and SHA-256 checksum). Both officers are interfaces -- the defaults are rule-based, but custom implementations slot in cleanly.

## What It Captures

Five sources: X bookmarks (via bird CLI), GitHub starred repos (REST API), Reddit saved posts (OAuth cookie), YouTube videos and playlists (yt-dlp with transcript extraction), and Audible highlights (local file scan). A Safari extension adds real-time capture from any page via context menu or keyboard shortcut.

## Where It Lives

- Go package: `go/internal/wraith/` (19 files, 4,732 LOC)
- Safari extension: `apps/MODUSBridge/`
- WebSocket bridge: `go/internal/server/wraith.go`
- Vault output: `brain/{source}/YYYY-MM-DD-{slug}.md`
