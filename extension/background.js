(function () {
  "use strict";

  var ext = typeof browser !== "undefined" ? browser : (typeof chrome !== "undefined" ? chrome : null);
  var DEFAULT_WS_URL = "ws://127.0.0.1:8781/wraith/ws";
  var AUTO_WS_URLS = [
    "ws://127.0.0.1:8781/wraith/ws",
    "ws://127.0.0.1:8782/wraith/ws",
    "ws://127.0.0.1:8783/wraith/ws",
    "ws://127.0.0.1:8780/wraith/ws"
  ];
  var WS_URL = DEFAULT_WS_URL;
  var RECONNECT_DELAY_MS = 3000;
  var explicitDaemonUrl = false;

  var socket = null;
  var reconnectTimer = null;
  var connectionState = {
    connected: false,
    connecting: false,
    lastError: null,
    lastEventAt: null
  };

  if (!ext || !ext.runtime) {
    return;
  }

  // --- Context Menu ---

  function setupContextMenus() {
    if (!ext.contextMenus) {
      return;
    }

    ext.contextMenus.removeAll(function () {
      ext.contextMenus.create({
        id: "modus-capture-page",
        title: "Send to MODUS",
        contexts: ["page", "frame"]
      });

      ext.contextMenus.create({
        id: "modus-capture-selection",
        title: "Send Selection to MODUS",
        contexts: ["selection"]
      });

      ext.contextMenus.create({
        id: "modus-capture-link",
        title: "Send Link to MODUS",
        contexts: ["link"]
      });
    });
  }

  if (ext.contextMenus && ext.contextMenus.onClicked) {
    ext.contextMenus.onClicked.addListener(function (info, tab) {
      if (!info || !tab) {
        return;
      }

      if (info.menuItemId === "modus-capture-page" || info.menuItemId === "modus-capture-selection") {
        captureTab(tab.id, info.selectionText || null);
      } else if (info.menuItemId === "modus-capture-link") {
        captureLink(info.linkUrl, tab);
      }
    });
  }

  setupContextMenus();

  // --- Capture Functions ---

  function captureTab(tabId, selectionText) {
    ext.tabs.sendMessage(tabId, { type: "extract_page" }, function (response) {
      var lastError = ext.runtime.lastError;
      if (lastError || !response) {
        log("Content script did not respond:", lastError ? lastError.message : "no response");
        // Fallback: capture basic tab info
        ext.tabs.get(tabId, function (tab) {
          sendCapture({
            source: "context-menu",
            url: tab ? tab.url : "",
            title: tab ? tab.title : "",
            selected: selectionText || "",
            bodyText: ""
          });
        });
        return;
      }

      sendCapture({
        source: "context-menu",
        url: response.url || "",
        title: response.title || "",
        bodyText: response.bodyText || "",
        selected: selectionText || "",
        author: response.author || "",
        siteName: response.siteName || "",
        headings: response.headings || [],
        links: response.links || [],
        images: response.images || [],
        tweet: response.tweet || null,
        meta: response.meta || {}
      });
    });
  }

  function captureLink(url, tab) {
    sendCapture({
      source: "context-menu",
      url: url,
      title: "Link from " + (tab ? tab.title : ""),
      meta: {
        referrer_url: tab ? tab.url : "",
        referrer_title: tab ? tab.title : ""
      }
    });
  }

  function sendCapture(payload) {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      log("Cannot send capture — not connected. Queuing locally.");
      queueLocally(payload);
      return;
    }

    try {
      socket.send(JSON.stringify({
        type: "capture",
        payload: payload,
        sentAt: nowIso()
      }));
      log("Capture sent:", truncateStr(payload.title || payload.url, 60));
    } catch (error) {
      log("Capture send failed:", String(error));
      queueLocally(payload);
    }
  }

  function queueLocally(payload) {
    if (!ext.storage || !ext.storage.local) {
      return;
    }
    ext.storage.local.get({ pendingCaptures: [] }, function (result) {
      var pending = result.pendingCaptures || [];
      payload._queuedAt = nowIso();
      pending.push(payload);
      // Cap at 200 local items
      if (pending.length > 200) {
        pending = pending.slice(-200);
      }
      ext.storage.local.set({ pendingCaptures: pending });
      log("Queued locally. Pending:", pending.length);
    });
  }

  function flushLocalQueue() {
    if (!ext.storage || !ext.storage.local) {
      return;
    }
    ext.storage.local.get({ pendingCaptures: [] }, function (result) {
      var pending = result.pendingCaptures || [];
      if (pending.length === 0) {
        return;
      }

      log("Flushing", pending.length, "queued captures");
      var i;
      for (i = 0; i < pending.length; i += 1) {
        delete pending[i]._queuedAt;
        sendCapture(pending[i]);
      }
      ext.storage.local.set({ pendingCaptures: [] });
    });
  }

  // --- WebSocket Connection ---

  function nowIso() {
    return new Date().toISOString();
  }

  function log() {
    var args = Array.prototype.slice.call(arguments);
    args.unshift("[MODUS Bridge]");
    console.log.apply(console, args);
  }

  function truncateStr(s, max) {
    if (!s) { return ""; }
    return s.length > max ? s.substring(0, max) + "..." : s;
  }

  function setConnectionState(nextState) {
    var key;
    for (key in nextState) {
      if (Object.prototype.hasOwnProperty.call(nextState, key)) {
        connectionState[key] = nextState[key];
      }
    }
    connectionState.lastEventAt = nowIso();
    persistStatus();
    updateActionTitle();
  }

  function snapshotStatus() {
    return {
      connected: !!connectionState.connected,
      connecting: !!connectionState.connecting,
      lastError: connectionState.lastError,
      lastEventAt: connectionState.lastEventAt,
      url: WS_URL
    };
  }

  function normalizeDaemonUrl(url) {
    var value = (url || "").trim();
    if (!value) {
      return DEFAULT_WS_URL;
    }
    if (!/^wss?:\/\//i.test(value)) {
      return null;
    }
    if (!/\/wraith\/ws$/i.test(value)) {
      value = value.replace(/\/+$/, "") + "/wraith/ws";
    }
    return value;
  }

  function wsToStatusUrl(wsUrl) {
    if (!wsUrl) {
      return "";
    }
    return wsUrl.replace(/^ws:\/\//i, "http://").replace(/^wss:\/\//i, "https://").replace(/\/wraith\/ws$/i, "/wraith/status");
  }

  function buildCandidateUrls() {
    var seen = {};
    var out = [];
    var i;
    function push(url) {
      var normalized = normalizeDaemonUrl(url);
      if (!normalized || seen[normalized]) {
        return;
      }
      seen[normalized] = true;
      out.push(normalized);
    }

    push(WS_URL);
    for (i = 0; i < AUTO_WS_URLS.length; i += 1) {
      push(AUTO_WS_URLS[i]);
    }
    return out;
  }

  function probeDaemon(wsUrl, cb) {
    var statusUrl = wsToStatusUrl(wsUrl);
    if (!statusUrl) {
      cb(false);
      return;
    }

    var xhr = new XMLHttpRequest();
    xhr.open("GET", statusUrl, true);
    xhr.timeout = 1200;
    xhr.onreadystatechange = function () {
      if (xhr.readyState !== 4) {
        return;
      }
      cb(xhr.status >= 200 && xhr.status < 300);
    };
    xhr.ontimeout = function () { cb(false); };
    xhr.onerror = function () { cb(false); };
    try {
      xhr.send();
    } catch (error) {
      cb(false);
    }
  }

  function autoSelectDaemonUrl(done) {
    if (explicitDaemonUrl) {
      done();
      return;
    }

    var candidates = buildCandidateUrls();
    var index = 0;
    function next() {
      if (index >= candidates.length) {
        done();
        return;
      }
      var candidate = candidates[index];
      index += 1;
      probeDaemon(candidate, function (ok) {
        if (ok) {
          if (candidate !== WS_URL) {
            log("Auto-selected daemon:", candidate);
          }
          WS_URL = candidate;
          done();
          return;
        }
        next();
      });
    }
    next();
  }

  function persistStatus() {
    if (ext.storage && ext.storage.local && ext.storage.local.set) {
      ext.storage.local.set({ bridgeStatus: snapshotStatus() });
    }
  }

  function updateActionTitle() {
    if (!ext.browserAction || !ext.browserAction.setTitle) {
      return;
    }

    var status = snapshotStatus();
    var title = "MODUS Bridge: disconnected";
    if (status.connecting) {
      title = "MODUS Bridge: connecting...";
    } else if (status.connected) {
      title = "MODUS Bridge: connected";
    }

    try {
      ext.browserAction.setTitle({ title: title });
    } catch (error) {
      // Safari sometimes throws on setTitle
    }
  }

  function scheduleReconnect() {
    if (reconnectTimer !== null) {
      return;
    }

    reconnectTimer = setTimeout(function () {
      reconnectTimer = null;
      connectSocket();
    }, RECONNECT_DELAY_MS);
  }

  function connectSocket() {
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
      return;
    }

    setConnectionState({
      connected: false,
      connecting: true,
      lastError: null
    });

    autoSelectDaemonUrl(function () {
      try {
        socket = new WebSocket(WS_URL);
      } catch (error) {
        setConnectionState({
          connected: false,
          connecting: false,
          lastError: String(error)
        });
        scheduleReconnect();
        return;
      }

      socket.onopen = function () {
        log("Connected to MODUS at", WS_URL);
        setConnectionState({
          connected: true,
          connecting: false,
          lastError: null
        });
        socket.send(JSON.stringify({
          type: "hello",
          payload: {
            userAgent: navigator.userAgent,
            extensionId: ext.runtime.id || null
          },
          sentAt: nowIso()
        }));
        // Flush any captures that were queued while disconnected
        flushLocalQueue();
      };

      socket.onmessage = function (event) {
        handleServerMessage(event.data);
      };

      socket.onerror = function () {
        setConnectionState({
          connected: false,
          connecting: false,
          lastError: "WebSocket error"
        });
      };

      socket.onclose = function () {
        log("Disconnected from MODUS");
        setConnectionState({
          connected: false,
          connecting: false
        });
        scheduleReconnect();
      };
    });
  }

  // --- Server Message Handling ---

  function handleServerMessage(raw) {
    var message;
    try {
      message = JSON.parse(raw);
    } catch (error) {
      log("Invalid JSON from server:", String(error));
      return;
    }

    var type = message.type || message.command || "";

    if (type === "hello_ack") {
      log("Server acknowledged connection. Version:", message.payload && message.payload.version);
      return;
    }

    if (type === "capture_ack") {
      var payload = message.payload || {};
      if (payload.ok) {
        log("Capture acknowledged. ID:", payload.id);
      } else {
        log("Capture rejected:", payload.error);
      }
      return;
    }

    if (type === "batch_ack") {
      var batchPayload = message.payload || {};
      log("Batch acknowledged:", batchPayload.enqueued, "/", batchPayload.total, "enqueued");
      return;
    }

    if (type === "ping") {
      socket.send(JSON.stringify({
        type: "command_result",
        payload: {
          id: message.id,
          ok: true,
          result: { pong: true, status: snapshotStatus() }
        }
      }));
      return;
    }

    if (type === "extract_page") {
      // Server wants us to extract the active tab
      ext.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        if (!tabs || !tabs.length) {
          return;
        }
        captureTab(tabs[0].id, null);
      });
      return;
    }

    if (type === "get_tabs") {
      ext.tabs.query({}, function (tabs) {
        var result = [];
        var i;
        tabs = tabs || [];
        for (i = 0; i < tabs.length; i += 1) {
          result.push({
            id: tabs[i].id,
            index: tabs[i].index,
            windowId: tabs[i].windowId,
            active: !!tabs[i].active,
            title: tabs[i].title || "",
            url: tabs[i].url || ""
          });
        }
        socket.send(JSON.stringify({
          type: "command_result",
          payload: { id: message.id, ok: true, result: result }
        }));
      });
      return;
    }

    if (type === "navigate") {
      var navParams = message.params || {};
      if (navParams.url) {
        ext.tabs.query({ active: true, currentWindow: true }, function (tabs) {
          if (tabs && tabs.length) {
            ext.tabs.update(tabs[0].id, { url: navParams.url });
          }
        });
      }
      return;
    }

    log("Unknown message type:", type);
  }

  // --- Bookmark Sync ---

  var bookmarkSyncState = {
    running: false,
    scrollCount: 0,
    capturedCount: 0,
    seenIds: {}
  };

  function startBookmarkSync(tabId) {
    if (bookmarkSyncState.running) {
      log("Bookmark sync already running");
      return;
    }

    bookmarkSyncState.running = true;
    bookmarkSyncState.scrollCount = 0;
    bookmarkSyncState.capturedCount = 0;

    // Load previously seen IDs from storage
    ext.storage.local.get({ bookmarkSeenIds: {} }, function (result) {
      bookmarkSyncState.seenIds = result.bookmarkSeenIds || {};
      log("Bookmark sync started. Previously seen:", Object.keys(bookmarkSyncState.seenIds).length);
      bookmarkSyncBatch(tabId);
    });
  }

  function bookmarkSyncBatch(tabId) {
    if (!bookmarkSyncState.running) {
      return;
    }

    ext.tabs.sendMessage(tabId, { type: "extract_bookmarks" }, function (response) {
      var lastError = ext.runtime.lastError;
      if (lastError || !response || !response.ok) {
        log("Bookmark sync: extraction failed, stopping.", lastError ? lastError.message : "");
        stopBookmarkSync();
        return;
      }

      var bookmarks = response.bookmarks;
      if (!bookmarks || !bookmarks._bookmarks || bookmarks._bookmarks.length === 0) {
        log("Bookmark sync: no tweets found, stopping.");
        stopBookmarkSync();
        return;
      }

      // Filter out already-seen tweets
      var newItems = [];
      var i;
      for (i = 0; i < bookmarks._bookmarks.length; i += 1) {
        var tweet = bookmarks._bookmarks[i];
        var id = tweet.tweet_id || tweet.handle + ":" + tweet.text.substring(0, 50);
        if (!bookmarkSyncState.seenIds[id]) {
          bookmarkSyncState.seenIds[id] = nowIso();
          newItems.push({
            source: "bookmark-sync",
            url: tweet.tweet_id ? "https://x.com/i/status/" + tweet.tweet_id : "",
            title: tweet.author + ": " + truncateStr(tweet.text, 120),
            bodyText: tweet.text,
            author: tweet.author,
            tweet: tweet
          });
        }
      }

      if (newItems.length > 0) {
        // Send as batch
        if (socket && socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({
            type: "bookmark_batch",
            payload: { items: newItems },
            sentAt: nowIso()
          }));
        }
        bookmarkSyncState.capturedCount += newItems.length;
        log("Bookmark sync: sent", newItems.length, "new. Total:", bookmarkSyncState.capturedCount);
      } else {
        log("Bookmark sync: all visible tweets already captured.");
      }

      // Save checkpoint
      ext.storage.local.set({ bookmarkSeenIds: bookmarkSyncState.seenIds });

      // Scroll down for more
      bookmarkSyncState.scrollCount += 1;
      if (bookmarkSyncState.scrollCount >= 50) {
        log("Bookmark sync: reached scroll limit (50). Stopping.");
        stopBookmarkSync();
        return;
      }

      // Tell the content script to scroll, then wait for new tweets to load
      ext.tabs.sendMessage(tabId, { type: "scroll_down" }, function () {
        setTimeout(function () {
          bookmarkSyncBatch(tabId);
        }, 2000); // Wait for X to render new tweets
      });
    });
  }

  function stopBookmarkSync() {
    bookmarkSyncState.running = false;
    log("Bookmark sync stopped.", bookmarkSyncState.capturedCount, "tweets captured in",
        bookmarkSyncState.scrollCount, "scrolls.");
  }

  // --- Internal Message Handling (from popup/sidepanel) ---

  ext.runtime.onMessage.addListener(function (message, sender, sendResponse) {
    if (!message || !message.type) {
      return;
    }

    if (message.type === "get_status") {
      sendResponse(snapshotStatus());
      return;
    }

    if (message.type === "connect_socket") {
      connectSocket();
      sendResponse({ ok: true });
      return;
    }

    if (message.type === "update_daemon_url") {
      var normalized = normalizeDaemonUrl(message.url);
      if (!normalized) {
        sendResponse({ ok: false, error: "Invalid WebSocket URL. Use ws:// or wss://." });
        return;
      }

      WS_URL = normalized;
      explicitDaemonUrl = true;
      if (ext.storage && ext.storage.local) {
        ext.storage.local.set({ daemonUrl: WS_URL });
      }

      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.close();
      } else {
        connectSocket();
      }

      sendResponse({ ok: true, url: WS_URL });
      return;
    }

    if (message.type === "capture_current") {
      ext.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        if (tabs && tabs.length) {
          captureTab(tabs[0].id, null);
          sendResponse({ ok: true });
        } else {
          sendResponse({ ok: false, error: "No active tab" });
        }
      });
      return true; // async
    }

    if (message.type === "start_bookmark_sync") {
      ext.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        if (tabs && tabs.length) {
          startBookmarkSync(tabs[0].id);
          sendResponse({ ok: true });
        } else {
          sendResponse({ ok: false, error: "No active tab" });
        }
      });
      return true;
    }

    if (message.type === "stop_bookmark_sync") {
      stopBookmarkSync();
      sendResponse({ ok: true });
      return;
    }

    if (message.type === "sync_status") {
      sendResponse({
        running: bookmarkSyncState.running,
        scrollCount: bookmarkSyncState.scrollCount,
        capturedCount: bookmarkSyncState.capturedCount,
        seenCount: Object.keys(bookmarkSyncState.seenIds).length
      });
      return;
    }
  });

  // --- Init ---
  updateActionTitle();
  if (ext.storage && ext.storage.local) {
    ext.storage.local.get("daemonUrl", function (result) {
      var stored = result && typeof result.daemonUrl === "string" ? result.daemonUrl : "";
      var normalized = normalizeDaemonUrl(stored);
      if (normalized) {
        WS_URL = normalized;
        explicitDaemonUrl = true;
      } else {
        WS_URL = DEFAULT_WS_URL;
        explicitDaemonUrl = false;
      }
      persistStatus();
      connectSocket();
    });
  } else {
    persistStatus();
    connectSocket();
  }
})();
