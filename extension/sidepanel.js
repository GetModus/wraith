// MODUS Bridge — Side Panel UI

const statusDot = document.getElementById("statusDot");
const statusText = document.getElementById("statusText");
const btnArchive = document.getElementById("btnArchive");
const btnResearch = document.getElementById("btnResearch");
const feedback = document.getElementById("feedback");
const activityLog = document.getElementById("activityLog");
const daemonUrlInput = document.getElementById("daemonUrl");
const btnSaveUrl = document.getElementById("btnSaveUrl");

// --- Status Polling ---

function updateStatus(connected) {
  if (connected) {
    statusDot.classList.add("connected");
    statusText.textContent = "Online";
  } else {
    statusDot.classList.remove("connected");
    statusText.textContent = "Disconnected";
  }
  btnArchive.disabled = !connected;
  btnResearch.disabled = !connected;
}

function checkStatus() {
  chrome.runtime.sendMessage({ type: "get_status" }, (response) => {
    if (chrome.runtime.lastError) {
      updateStatus(false);
      return;
    }
    updateStatus(response?.connected || false);
  });
}

// Poll status every 3 seconds
checkStatus();
setInterval(checkStatus, 3000);

// Listen for status broadcasts from background
chrome.runtime.onMessage.addListener((msg) => {
  if (msg.type === "status") {
    updateStatus(msg.connected);
  }
});

// --- Actions ---

function showFeedback(message, type = "success") {
  feedback.innerHTML = `<div class="feedback-msg ${type}">${message}</div>`;
  setTimeout(() => {
    feedback.innerHTML = "";
  }, 4000);
}

btnArchive.addEventListener("click", async () => {
  btnArchive.disabled = true;
  showFeedback("Filing...", "success");

  chrome.runtime.sendMessage({ type: "send_to_archive" }, (response) => {
    if (chrome.runtime.lastError) {
      showFeedback("Lost contact with background worker.", "error");
      btnArchive.disabled = false;
      return;
    }

    if (response?.error) {
      showFeedback(response.error, "error");
    } else {
      showFeedback(`Archived: ${response.title || "page"}`);
      addLogEntry(`Archived: ${response.title || "page"}`);
    }
    btnArchive.disabled = false;
  });
});

btnResearch.addEventListener("click", async () => {
  btnResearch.disabled = true;
  showFeedback("Dispatching research request...", "success");

  chrome.runtime.sendMessage({ type: "research_page" }, (response) => {
    if (chrome.runtime.lastError) {
      showFeedback("Lost contact with background worker.", "error");
      btnResearch.disabled = false;
      return;
    }

    if (response?.error) {
      showFeedback(response.error, "error");
    } else {
      showFeedback(`Research queued: ${response.title || "page"}`);
      addLogEntry(`Research: ${response.title || "page"}`);
    }
    btnResearch.disabled = false;
  });
});

// --- Settings ---

chrome.storage.local.get("daemonUrl", (result) => {
  daemonUrlInput.value = result.daemonUrl || "ws://127.0.0.1:8781/wraith/ws";
});

btnSaveUrl.addEventListener("click", () => {
  const url = daemonUrlInput.value.trim();
  if (!url) return;

  chrome.runtime.sendMessage({ type: "update_daemon_url", url }, (response) => {
    if (response?.ok) {
      showFeedback("Daemon URL updated. Reconnecting.");
    } else {
      showFeedback("Failed to update URL.", "error");
    }
  });
});

// --- Activity Log ---

function addLogEntry(message) {
  const timestamp = new Date().toLocaleTimeString();

  // Remove the "standing by" placeholder
  const empty = activityLog.querySelector(".log-empty");
  if (empty) empty.remove();

  const entry = document.createElement("div");
  entry.className = "log-entry";
  entry.innerHTML = `
    <div class="log-message">${escapeHtml(message)}</div>
    <div class="log-time">${timestamp}</div>
  `;

  activityLog.prepend(entry);

  // Keep max 10 visible
  while (activityLog.children.length > 10) {
    activityLog.removeChild(activityLog.lastChild);
  }
}

function loadActivityLog() {
  chrome.storage.local.get("activityLog", (result) => {
    const log = result.activityLog || [];
    if (log.length === 0) return;

    activityLog.innerHTML = "";
    log.slice(0, 10).forEach((entry) => {
      const div = document.createElement("div");
      div.className = "log-entry";
      const time = new Date(entry.timestamp).toLocaleTimeString();
      div.innerHTML = `
        <div class="log-message">${escapeHtml(entry.message)}</div>
        <div class="log-time">${time}</div>
      `;
      activityLog.appendChild(div);
    });
  });
}

function escapeHtml(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

loadActivityLog();
