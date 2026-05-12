const WS_URL = "ws://127.0.0.1:8778/trace-bridge/ws";
const HEARTBEAT_INTERVAL = 15_000;
const RECONNECT_BASE = 1_000;
const RECONNECT_MAX = 30_000;

let ws = null;
let heartbeatTimer = undefined;
let reconnectDelay = RECONNECT_BASE;

function sourcePattern(source) {
  if (source === "x-bookmarks") return "*://x.com/i/bookmarks*";
  if (source === "reddit-saved") return "*://old.reddit.com/user/*/saved*";
  if (source === "grok") return "*://grok.x.ai/*";
  return null;
}

function send(value) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(value));
  }
}

function commandResult(id, ok, result, error) {
  const payload = { id, ok };
  if (result !== undefined) payload.result = result;
  if (error) payload.error = error;
  return { type: "command_result", payload };
}

async function activeTab() {
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

async function targetTab(params = {}, source) {
  const rawTabId = params.tabId;
  const tabId = typeof rawTabId === "number" ? rawTabId : typeof rawTabId === "string" ? Number.parseInt(rawTabId, 10) : undefined;
  if (Number.isFinite(tabId)) {
    const tabs = await chrome.tabs.query({});
    return tabs.find((tab) => tab.id === tabId) || null;
  }
  if (source) {
    const pattern = sourcePattern(source);
    if (pattern) {
      const tabs = await chrome.tabs.query({ url: pattern });
      return tabs[0] || null;
    }
  }
  return activeTab();
}

async function sendToTab(tab, message) {
  if (!tab || !tab.id) {
    throw new Error("no target tab available");
  }
  return chrome.tabs.sendMessage(tab.id, message);
}

async function handleCommand(msg) {
  const params = msg.params || {};
  if (msg.command === "ping") {
    return { pong: true, extensionId: chrome.runtime.id, at: new Date().toISOString() };
  }
  if (msg.command === "get_tabs") {
    return chrome.tabs.query({});
  }
  if (msg.command === "navigate") {
    const url = typeof params.url === "string" ? params.url : "";
    if (!url) throw new Error("navigate requires url");
    const tab = await targetTab(params);
    if (tab && tab.id) return chrome.tabs.update(tab.id, { url, active: true });
    throw new Error("no tab available for navigate");
  }
  if (msg.command === "extract_page" || msg.command === "capture_page") {
    return sendToTab(await targetTab(params), { type: "extract_page" });
  }
  if (msg.command === "scrape_source") {
    const source = typeof params.source === "string" ? params.source : "";
    if (!source) throw new Error("scrape_source requires source");
    return sendToTab(await targetTab(params, source), { type: "scrape", source, params });
  }
  if (msg.command === "query_source") {
    const source = typeof params.source === "string" ? params.source : "";
    const query = typeof params.query === "string" ? params.query : "";
    if (!source || !query) throw new Error("query_source requires source and query");
    return sendToTab(await targetTab(params, source), { type: "query", source, query, params });
  }
  throw new Error(`unknown command: ${msg.command}`);
}

async function handleServerMessage(msg) {
  if (msg && typeof msg.id === "string" && typeof msg.command === "string") {
    try {
      send(commandResult(msg.id, true, await handleCommand(msg)));
    } catch (error) {
      send(commandResult(msg.id, false, undefined, error instanceof Error ? error.message : String(error)));
    }
    return;
  }
  if (msg && typeof msg.id === "string" && (msg.type === "scrape" || msg.type === "query")) {
    try {
      const command =
        msg.type === "scrape"
          ? { id: msg.id, command: "scrape_source", params: { source: msg.source, ...(msg.params || {}) } }
          : { id: msg.id, command: "query_source", params: { source: msg.source, query: msg.query || "", ...(msg.params || {}) } };
      send(commandResult(msg.id, true, await handleCommand(command)));
    } catch (error) {
      send(commandResult(msg.id, false, undefined, error instanceof Error ? error.message : String(error)));
    }
  }
}

function cleanup() {
  if (heartbeatTimer !== undefined) {
    clearInterval(heartbeatTimer);
  }
  heartbeatTimer = undefined;
  ws = null;
}

function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return;
  }
  ws = new WebSocket(WS_URL);
  ws.addEventListener("open", () => {
    reconnectDelay = RECONNECT_BASE;
    send({ type: "hello", version: "2.0-ts", extensionId: chrome.runtime.id, sources: ["x-bookmarks", "reddit-saved", "grok", "current-page"] });
    if (heartbeatTimer !== undefined) clearInterval(heartbeatTimer);
    heartbeatTimer = setInterval(() => send({ type: "heartbeat", at: new Date().toISOString() }), HEARTBEAT_INTERVAL);
  });
  ws.addEventListener("message", (event) => {
    try {
      void handleServerMessage(JSON.parse(String(event.data)));
    } catch {
      send({ type: "error", error: "bad_server_json" });
    }
  });
  ws.addEventListener("close", () => {
    cleanup();
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX);
  });
}

connect();
