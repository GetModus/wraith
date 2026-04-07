# Release Process

## Version Tagging

WRAITH is an independent module with its own version. Tag format:

```
git tag -a v0.X.Y -m "description"
```

The Safari extension has its own version in `extension/manifest.json` (currently `1.0.0`). Update this separately when the extension message format changes.

## Changelog Format

Maintain `CHANGELOG.md` in the repo root. Group by category using [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.

## Pre-Release Verification

Before tagging a release:

1. **Tests pass**: `go test ./internal/... -v` — all WRAITH tests green (wraith, mcp, server, markdown)
2. **Both binaries build**: `go build ./cmd/wraith-bridge/` and `go build ./cmd/wraith-mcp/`
3. **Bridge starts**: `./wraith-bridge` starts and `/wraith/status` responds
4. **MCP starts**: `./wraith-mcp` starts and responds to MCP tool calls
5. **Extension connects**: Safari extension shows connected in the sidepanel (bridge mode)
6. **Capture round-trip (bridge)**: Right-click "Send to MODUS" on a page, verify queue and vault file
7. **Capture round-trip (MCP)**: Call `wraith_capture` then `wraith_process`, verify vault file
8. **Source ingestion**: Run at least one batch source and verify state records appear
9. **Smoke test**: `make smoke` passes all 4 phases

## Build Commands

```bash
# Build both binaries
go build -o wraith-bridge ./cmd/wraith-bridge/
go build -o wraith-mcp ./cmd/wraith-mcp/

# Run all tests
go test ./internal/... -v

# Build with version info (optional)
go build -ldflags "-X main.version=$(git describe --tags)" -o wraith-bridge ./cmd/wraith-bridge/
```

## Binary Size

Each binary is approximately 6-8 MB. WRAITH has no CGo dependencies and minimal third-party imports (only `gorilla/websocket` for the bridge). The MCP binary is slightly smaller (no websocket dependency).

## Entrypoints

| Binary | Path | Purpose |
|--------|------|---------|
| `wraith-bridge` | `cmd/wraith-bridge/main.go` | WebSocket server for Safari extension capture |
| `wraith-mcp` | `cmd/wraith-mcp/main.go` | MCP server for harness capture (Claude Code, Cursor, etc.) |

## Extension Distribution

The Safari extension is built as part of the MODUS Bridge Xcode project at `apps/MODUSBridge/`. It is distributed as a macOS app containing the extension. Build via Xcode or:

```bash
xcodebuild -project "apps/MODUSBridge/MODUS Bridge.xcodeproj" -scheme "MODUS Bridge" build
```

The extension is not distributed via the App Store. It is a local development tool.
