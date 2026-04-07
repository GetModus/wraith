# Testing

## Existing Test Coverage

WRAITH has 14 tests across 4 test files in `go/internal/wraith/`:

| File | Tests | Coverage |
|------|-------|----------|
| `queue_test.go` | 3 | Queue ingress dedup (fingerprint), ingest ledger transitions, near-duplicate fold |
| `consumer_test.go` | 7 | Discard skips librarian, keep files via librarian, YouTube video direct routing, YouTube playlist direct routing, watch+list prefers single-video, YouTube URL helper classification (with handoff JSONL verification) |
| `acceptance_test.go` | 3 | Full capture-to-file pipeline, failure path (librarian error), state-level dedup |
| `fixture_replay_test.go` | 1 | Replays all fixture captures through queue, verifies fingerprint and near-duplicate dedup |

All tests use `t.TempDir()` for isolated data and vault directories. No external services are required -- tests inject mock officers (`testScoutOfficer`, `testLibrarianOfficer`, `failingLibrarianOfficer`) to control pipeline behavior. YouTube routing tests use function-variable stubs (`ingestYouTubeVideoFn`, `ingestYouTubeFn`) to avoid real yt-dlp calls.

## Running Tests

```bash
# All WRAITH tests
go test ./internal/... -v

# Core pipeline tests only
go test ./internal/wraith/ -v

# MCP tool tests
go test ./internal/mcp/ -v

# Server tests
go test ./internal/server/ -v
```

To run a specific test:

```bash
go test ./internal/wraith/ -run TestAcceptanceCaptureToQueueScoutLibrarianFile -v
```

## Build Verification

```bash
# Both binaries
go build -o wraith-bridge ./cmd/wraith-bridge/
go build -o wraith-mcp ./cmd/wraith-mcp/
```

## Fixture Usage

The `wraith/fixtures/` directory contains two subdirectories:

- `fixtures/captures/` -- 9 capture payloads: `page-basic.json`, `page-long.json`, `link.json`, `selection.json`, `tweet.json`, `duplicate-exact-a.json`, `duplicate-exact-b.json`, `duplicate-near-a.json`, `duplicate-near-b.json`.
- `fixtures/expected/` -- 3 expected outcome files: `queue-state-after-ingest.json`, `dedup-outcomes.json`, `handoff-records.jsonl`.

The `fixture_replay_test.go` test loads all capture fixtures, enqueues them, and verifies that exact duplicates (fingerprint match) and near-duplicates (Jaccard similarity ≥ 0.82) are correctly identified.

## Adding New Tests

Follow the established pattern:

1. Create temp directories with `t.TempDir()` for data and vault
2. Open a queue and state against the temp data directory
3. Inject captures via `queue.Enqueue`
4. Use `ProcessQueueWithOfficers` with custom officers to control classification and filing
5. Assert on capture status, ingest state, vault file existence, and handoff JSONL contents

For testing ingestion sources (IngestX, IngestGitHub, etc.), external dependencies make unit testing harder. These are better covered by integration tests that mock the HTTP clients or use recorded responses in `fixtures/captures/`.

## Smoke Test

The smoke test at `wraith/scripts/smoke.sh` runs 4 phases:

1. **Fixture validation** -- Confirms all 9 capture and 3 expected fixture files exist and are valid JSON.
2. **JSON syntax** -- `python3 -m json.tool` on every fixture file.
3. **Go tests** -- Runs `go test -tags nollamacpp ./internal/wraith/` and verifies all pass.
4. **Fixture replay dedup** -- Runs `TestFixtureReplayDedup`, which loads all capture fixtures, enqueues them, and verifies at least 1 fingerprint dedup and 1 near-duplicate dedup occur. Checks exact-b is deduped against exact-a, and near-b against near-a (Jaccard ~0.97).

Run with:
```bash
cd wraith && make smoke
# or directly:
cd wraith && ./scripts/smoke.sh
```

## YouTube Routing Tests

The consumer test file covers YouTube URL special-casing:

- `TestProcessQueueRoutesYouTubeVideoDirectly` -- Watch URL routes to `IngestYouTubeVideo`, not Scout/Librarian pipeline.
- `TestProcessQueuePrefersYouTubeVideoOverPlaylistContext` -- Watch URL with `list=` parameter still routes as single video.
- `TestProcessQueueRoutesYouTubePlaylistDirectly` -- Playlist URL routes to `IngestYouTube`, extracts playlist ID.
- `TestYouTubeRoutingHelpers` -- Table-driven test for `isYouTubeWatchURL()` and `extractYouTubePlaylistID()` across youtube.com, youtu.be, and non-YouTube URLs.

These tests use function-variable stubs to avoid real yt-dlp calls while verifying correct routing logic.
