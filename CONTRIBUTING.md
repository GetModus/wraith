# Contributing to WRAITH

WRAITH is part of the MODUS ecosystem. Contributions are welcome.

## Development Setup

```bash
# Go 1.22+ required
go version

# Build the bridge
go build -o wraith-bridge ./cmd/wraith-bridge/

# Run all tests
go test ./internal/... -v

# Run fixture smoke test
./scripts/smoke.sh
```

## Code Style

- Go: `gofmt` and `go vet` must pass with no warnings.
- JavaScript (extension): No framework, vanilla JS. Keep extraction functions pure.
- Tests: Every new pipeline feature needs a test in `internal/wraith/`. Use `t.TempDir()` for isolation.

## Pull Request Checklist

- [ ] `go test ./internal/... -v` passes
- [ ] `go vet ./...` clean
- [ ] Fixture smoke test passes if queue/dedup behavior changed
- [ ] New ingestion sources include a dedup key strategy
- [ ] Officer pipeline changes include handoff JSONL verification
- [ ] No sensitive data (real URLs, cookies, API keys) in test fixtures

## Architecture Constraints

- **No cloud dependencies.** WRAITH is local-first. Do not add external service calls that aren't explicitly configured by the user.
- **No content rewriting.** The Librarian files content as-is. YouTube transcript extraction produces structured sections but does not editorialize. Generic captures are filed verbatim.
- **Auditable pipeline.** Every capture that enters the queue must have a traceable path through the state machine, recorded in ingest_history.
- **Deterministic dedup.** Fingerprint and Jaccard dedup must be reproducible given the same input. No randomness in dedup decisions.

## Reporting Issues

Open an issue at [github.com/GetModus/wraith](https://github.com/GetModus/wraith). Include:
- WRAITH version / commit hash
- Steps to reproduce
- Expected vs actual behavior
- Relevant queue/state/handoff file excerpts (redact any personal URLs)
