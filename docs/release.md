# Release Process

## Version Tagging

WRAITH is part of the MODUS Go binary. It does not have independent versioning. Releases follow the MODUS version tag format:

```
git tag -a v0.X.Y -m "description"
```

The Safari extension has its own version in `apps/MODUSBridge/MODUS Bridge/MODUS Bridge Extension/Resources/manifest.json` (currently `1.0.0`). Update this separately when the extension message format changes.

## Changelog Format

Maintain a section in the release notes for WRAITH changes. Group by category:

```
### WRAITH
- Added: new ingestion source for X
- Changed: near-dedup threshold from 0.80 to 0.82
- Fixed: Reddit token_v2 cookie extraction on Safari 18
```

## Pre-Release Verification

Before tagging a release:

1. **Tests pass**: `go test ./internal/wraith/ -v` -- all current WRAITH tests green
2. **Build succeeds**: `go build ./cmd/modus/` completes without errors
3. **Server starts**: The binary starts and `/wraith/status` responds
4. **Extension connects**: Safari extension shows connected in the sidepanel
5. **Capture round-trip**: Right-click "Send to MODUS" on a page, verify the queue shows a new item and processing produces a vault file
6. **Source ingestion**: Run at least one batch source (e.g., GitHub with a small `maxItems`) and verify state records appear

## Build Commands

```bash
cd go

# Build the binary
go build -o modus ./cmd/modus/

# Run tests
go test ./internal/wraith/ -v

# Build with version info (optional)
go build -ldflags "-X main.version=$(git describe --tags)" -o modus ./cmd/modus/
```

## Binary Size

The MODUS binary includes the full agent framework, MCP server, web console, and WRAITH. Expect approximately 15-25 MB depending on build flags. WRAITH itself contributes a small fraction -- it has no CGO dependencies and minimal third-party imports (only `gorilla/websocket` for the bridge).

## Extension Distribution

The Safari extension is built as part of the MODUS Bridge Xcode project at `apps/MODUSBridge/`. It is distributed as a macOS app containing the extension. Build via Xcode or:

```bash
xcodebuild -project "apps/MODUSBridge/MODUS Bridge.xcodeproj" -scheme "MODUS Bridge" build
```

The extension is not distributed via the App Store. It is a local development tool.
