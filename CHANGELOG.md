# Changelog

All notable changes to WRAITH will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.2.0] — 2026-04-07

### Added
- **MCP control surface.** Standalone `wraith-mcp` binary exposes the full pipeline as MCP tool calls: `wraith_status`, `wraith_queue`, `wraith_capture`, `wraith_process`, `wraith_sources`. Any MCP-compatible harness (Claude Code, Cursor, Windsurf) can now enqueue captures, inspect queue state, and trigger processing — no browser required.
- MCP server entry point at `cmd/wraith-mcp/main.go`
- MCP tool registrations in `internal/mcp/wraith.go`
- Dual-mode architecture: browser bridge and MCP server share the same queue, state, officers, and vault

### Changed
- README updated with two runtime modes, MCP configuration example, and "Use WRAITH from Another Harness" section
- Architecture docs reflect MCP as a second control surface, not a second implementation
- Release process updated for both `wraith-bridge` and `wraith-mcp` binaries
- Testing docs cover MCP tool tests and both build targets

## [0.1.0] — 2026-04-07

### Added
- Repository packaging: README, docs, fixtures, CI, scripts
- Deterministic test fixtures for capture replay and dedup verification
- Smoke test script for fixture-based regression checks
- Claims and safety documentation with verified/non-verified separation
- SVG architecture diagrams (ingest flow, runtime topology, queue state machine, officer handoff)
- Hero banner SVG with shield badges
- GitHub Actions CI workflow for lint, vet, and test

### Existing (pre-packaging)
- Durable JSON capture queue with fingerprint + Jaccard near-duplicate dedup
- Scout → Librarian officer pipeline with JSONL audit trail
- Safari extension with WebSocket bridge, auto-reconnect, local queue fallback
- Five ingestion sources: X/Twitter, GitHub, Reddit, YouTube, Audible
- YouTube URL routing: watch URLs → single-video ingest, playlist URLs → playlist ingest
- YouTube Librarian extraction: structured sections from transcripts via Gemma 4 26B
- Triage classification (ADAPT/KEEP/MORE_INFO/DISCARD) via Librarian LLM
- 15 unit/integration/acceptance tests across 4 test files
