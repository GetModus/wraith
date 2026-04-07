# Changelog

All notable changes to WRAITH will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Repository packaging: README, docs, fixtures, CI, scripts
- Deterministic test fixtures for capture replay and dedup verification
- Smoke test script for fixture-based regression checks
- Claims and safety documentation with verified/non-verified separation
- SVG architecture diagrams (ingest flow, runtime topology, queue state machine, officer handoff)
- GitHub Actions CI workflow for lint, vet, and test

### Existing (pre-packaging)
- Durable JSON capture queue with fingerprint + Jaccard near-duplicate dedup
- Scout → Librarian officer pipeline with JSONL audit trail
- Safari extension with WebSocket bridge, auto-reconnect, local queue fallback
- Five ingestion sources: X/Twitter, GitHub, Reddit, YouTube, Audible
- Triage classification (ADAPT/KEEP/MORE_INFO/DISCARD) via Librarian LLM
- Heartbeat cadences (ingestion 2h, triage 4h)
- 8 unit/integration/acceptance tests across 3 test files
