# WRAITH Repository Readiness Report

**Date:** 2026-04-07
**Prepared by:** MODUS (automated packaging pass)
**Status:** Ready for review

---

## Files Created/Updated

### Root (7 files)
| File | Purpose | Status |
|------|---------|--------|
| `README.md` | Authoritative product doc | Created |
| `LICENSE` | MIT license | Created |
| `CONTRIBUTING.md` | Dev setup, PR checklist | Created |
| `SECURITY.md` | Security scope, reporting | Created |
| `CHANGELOG.md` | Release history | Created |
| `.gitignore` | Data/build exclusions | Created |
| `.editorconfig` | Formatting rules | Created |

### Documentation (10 files)
| File | Words | Status |
|------|-------|--------|
| `docs/overview.md` | ~300 | Created |
| `docs/architecture.md` | ~700 | Created |
| `docs/bridge-extension.md` | ~600 | Created |
| `docs/queue-and-dedup.md` | ~650 | Created |
| `docs/scout-librarian-pipeline.md` | ~500 | Created |
| `docs/operations.md` | ~450 | Created |
| `docs/troubleshooting.md` | ~400 | Created |
| `docs/testing.md` | ~300 | Created |
| `docs/release.md` | ~250 | Created |
| `docs/claims-and-safety.md` | ~700 | Created |

### Diagrams (4 files)
| File | Content | Status |
|------|---------|--------|
| `docs/diagrams/ingest-flow.svg` | End-to-end capture flow | Created |
| `docs/diagrams/runtime-topology.svg` | Runtime components and ports | Created |
| `docs/diagrams/queue-state-machine.svg` | Capture state transitions | Created |
| `docs/diagrams/officer-handoff.svg` | Scout → Librarian decision tree | Created |

### Test Fixtures (12 files)
| File | Purpose | Status |
|------|---------|--------|
| `fixtures/captures/page-basic.json` | Simple page capture | Created, valid JSON |
| `fixtures/captures/page-long.json` | Long article with images | Created, valid JSON |
| `fixtures/captures/link.json` | Link-only capture | Created, valid JSON |
| `fixtures/captures/selection.json` | Text selection capture | Created, valid JSON |
| `fixtures/captures/tweet.json` | X/Twitter bookmark | Created, valid JSON |
| `fixtures/captures/duplicate-exact-a.json` | Exact dedup pair A | Created, valid JSON |
| `fixtures/captures/duplicate-exact-b.json` | Exact dedup pair B | Created, valid JSON |
| `fixtures/captures/duplicate-near-a.json` | Near dedup pair A | Created, valid JSON |
| `fixtures/captures/duplicate-near-b.json` | Near dedup pair B | Created, valid JSON |
| `fixtures/expected/queue-state-after-ingest.json` | Expected queue stats | Created, valid JSON |
| `fixtures/expected/dedup-outcomes.json` | Expected dedup results | Created, valid JSON |
| `fixtures/expected/handoff-records.jsonl` | Expected handoff records | Created |

### Scripts & CI (3 files)
| File | Purpose | Status |
|------|---------|--------|
| `Makefile` | 8 operator targets | Created |
| `scripts/smoke.sh` | 4-phase fixture smoke test | Created, executable |
| `.github/workflows/wraith-ci.yml` | GitHub Actions CI | Created |

### Go Test File (1 file)
| File | Purpose | Status |
|------|---------|--------|
| `go/internal/wraith/fixture_replay_test.go` | Fixture replay dedup test | Created |

---

## Commands Run and Results

### Existing test baseline
```
go test -tags nollamacpp ./internal/wraith/ -v -count=1
# 13/13 PASS (8 pre-existing + 5 YouTube routing/helper tests)
```

### Fixture JSON validation
```
for f in wraith/fixtures/**/*.json; do python3 -m json.tool "$f" > /dev/null; done
# 11/11 valid
```

### Full test suite (with fixture replay)
```
go test -tags nollamacpp ./internal/wraith/ -v -count=1
# 15/15 PASS (14 original + 1 fixture replay)
```

### Smoke test
```
cd wraith && ./scripts/smoke.sh
# Phase 1: 9 capture fixtures, 3 expected fixtures — PASS
# Phase 2: All fixture JSON valid — PASS
# Phase 3: 15/15 Go tests pass — PASS
# Phase 4: Fixture replay dedup verification — PASS
#   - duplicate-exact-b: fingerprint deduped against exact-a
#   - duplicate-near-b: Jaccard near-deduped against near-a (sim=0.97)
#   - Queue stats: total=9 queued=7 deduped=2
```

### Build check
```
go build -tags nollamacpp ./...
# Clean compile, no errors
```

---

## Verified vs Inferred Statements

### Verified (from code or test execution)
- Queue fingerprint dedup works (SHA-256 match, tested)
- Near-duplicate Jaccard threshold is 0.82 (code: queue.go:376, tested at 0.97)
- Scout discard prevents Librarian filing (tested)
- Librarian write failure preserves capture in queue (tested)
- Handoff JSONL records are written for all processed captures (tested)
- Extension auto-reconnects every 3 seconds (code: background.js RECONNECT_DELAY_MS = 3000)
- Local queue fallback stores up to 200 captures (code: background.js)
- Content extraction limits: 100 headings, 300 links, 50 images, 50KB body (code: content.js)
- YouTube watch URLs route to dedicated single-video ingest before Scout (tested: 4 consumer tests)
- YouTube playlist URLs route to dedicated playlist ingest (tested)
- Watch URLs with `list=` parameter prefer single-video over playlist (tested)
- YouTube ingest includes Librarian extraction when transcript available (code: ingest_yt.go:111-119, 431-439)
- Both YouTube paths write to `brain/youtube/` (code: ingest_yt.go)
- Bridge origin allowlist admits `safari-web-extension://` schemes (code: server.go, live-verified: 403 fix)
- WebSocket reconnect works after disconnect (live probe: Phase 4a of bridge stabilization)

### Inferred (from code reading, not runtime verification)
- WebSocket bridge accepts only one connection at a time (code: wraith.go:93-95, not tested with concurrent connections)
- Safari cookie parsing works for current macOS version (no test for binary cookie format changes)
- Heartbeat cadences execute on schedule (code reads correctly, but cadence timing not integration-tested)
- Triage classification quality depends on Librarian LLM availability (no mock test for triage path)
- YouTube Librarian extraction quality depends on transcript availability and Gemma 4 26B on :8090

---

## Residual Risks

1. **No integration test for WebSocket bridge.** `go/internal/server/wraith.go` has a basic status test but no WebSocket handshake test. Risk: protocol changes in gorilla/websocket could break the bridge without test coverage.

2. **No test for Safari cookie parsing.** `cookies.go` parses Apple's binary cookie format. Format changes in future macOS versions would go undetected until runtime failure.

3. **Triage path untested offline.** Triage requires llama-server on :8090. No mock/stub test exists for the triage classification pipeline.

4. **Fixture expected outcomes not machine-asserted.** `fixtures/expected/queue-state-after-ingest.json` documents expected stats but the fixture replay test only checks `deduped >= 1`, not exact counts.

5. **Extension not testable in CI.** Safari extension requires macOS + Safari. No headless testing approach exists.

6. ~~**Default Scout discards tweets.**~~ **FIXED.** Scout now checks `c.Tweet != nil && c.Tweet.Text != ""` before classifying as empty discard. Test: `TestScoutKeepsTweetWithTextButEmptyBody` in `consumer_test.go`.

7. **Server binding was `0.0.0.0`, not `127.0.0.1`.** **FIXED.** `go/cmd/modus/main.go` `cmdServe` and `cmdBridge` now bind to `127.0.0.1:{port}` explicitly. SECURITY.md and claims doc assertions are now accurate.

8. **YouTube auth surface was misstated.** **FIXED.** README and SECURITY.md previously claimed `YOUTUBE_API_KEY` was required. YouTube ingestion uses yt-dlp with no API key. Docs corrected.

---

## Launch Checklist

- [x] README with product definition, flow, endpoints, quickstart
- [x] Claims doc separating verified/non-verified/roadmap
- [x] 9 deterministic test fixtures (synthetic, no real user data)
- [x] 4 SVG architecture diagrams
- [x] Smoke test passing all 4 phases
- [x] 15/15 Go tests passing
- [x] CI workflow (vet, gofmt, test, fixture validation, build)
- [x] Security policy with known considerations
- [x] Contribution guide with architecture constraints
- [ ] TODO: WebSocket integration test
- [ ] TODO: Triage pipeline mock test
- [ ] TODO: Machine-assert exact fixture expected outcomes
- [ ] TODO: Human review of all docs for accuracy
- [x] Repo name decided: `GetModus/wraith`
- [x] MCP control surface documented in README, architecture, operations, release, claims docs
- [x] `wraith-mcp` binary entry point at `cmd/wraith-mcp/main.go`
- [x] 5 MCP tools registered: wraith_status, wraith_queue, wraith_capture, wraith_process, wraith_sources
- [x] CHANGELOG updated with v0.1.0 and v0.2.0 entries
