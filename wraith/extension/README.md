# Trace Bridge — Safari Web Extension

Browser bridge between Safari and the local Trace Bridge service. Trace Bridge
is transport and capture; WRAITH remains the governed intake boundary behind it.

Status: active day-to-day bridge path. The extension connects to
`src/wraith-bridge.ts` at `ws://127.0.0.1:8778/trace-bridge/ws`.

## Building the Safari Extension

Safari Web Extensions need a native app wrapper. Xcode generates this for you:

1. Open Xcode. **File > New > Project > Safari Extension App**.
2. Set product name to `Trace Bridge`, team to your dev account.
3. Choose **"Web Extension"** when prompted for the extension type.
4. Xcode creates a boilerplate `Resources/` folder — replace its contents with the files from this directory (`manifest.json`, `background.js`, `content.js`).
5. Build and run (Cmd+R). Xcode installs both the container app and the extension.

Typed source lives under `src/`. The manifest-loaded JavaScript is kept in this
directory for Safari compatibility and should stay aligned with the TypeScript
source.

The Safari manifest intentionally uses a background script and broad page match
for local day-to-day capture. Safari did not start the MV3 service worker
reliably for this bridge lane during live testing.

## Enabling in Safari

1. **Safari > Settings > Extensions** (Cmd+,).
2. Check the box next to Trace Bridge.
3. Grant permissions when prompted for the listed domains.
4. Open the relevant pages (x.com/i/bookmarks, old.reddit.com, grok.x.ai) so content scripts load.

## Verifying the Connection

The extension is configured to connect to `ws://127.0.0.1:8778/trace-bridge/ws`.

```bash
npm run modus -- wraith bridge start
npm run modus -- wraith bridge status
npm run modus -- wraith bridge ping
npm run modus -- wraith bridge tabs
npm run modus -- wraith bridge capture --apply
```

The local bridge is also installed as a user LaunchAgent:

```bash
launchctl print gui/$(id -u)/com.modus.wraith.bridge
```

Or inspect directly:

```bash
curl http://127.0.0.1:8778/wraith/status
curl http://127.0.0.1:8778/trace-bridge/status
```

Status should show the extension as a connected client once Safari has loaded
the extension and the bridge server is running. You can also open Safari's
**Develop > Web Extension Background Pages > Trace Bridge** to see console logs from
`background.js`.

## Architecture

```
Safari tab (content.js)
    ^
    |  chrome.runtime messages
    v
background.js  <── WebSocket ──>  src/wraith-bridge.ts (ws://127.0.0.1:8778/trace-bridge/ws)
```

Commands flow from the TypeScript bridge to `background.js`, get routed to the
correct tab's `content.js`, and results come back the same path. Durable capture
then routes through `src/wraith-intake.ts` and remains WRAITH review material.
