function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function text(el) {
  return el && el.textContent ? el.textContent.replace(/\s+/g, " ").trim() : "";
}

function pageSnapshot() {
  const descriptionEl = document.querySelector('meta[name="description"], meta[property="og:description"]');
  return {
    title: document.title || window.location.href,
    url: window.location.href,
    text: (document.body && document.body.innerText ? document.body.innerText.replace(/\s+/g, " ").trim() : "").slice(0, 12000),
    selection: (window.getSelection() ? window.getSelection().toString().trim() : ""),
    description: descriptionEl ? descriptionEl.getAttribute("content") || "" : "",
  };
}

async function scrapeXBookmarks() {
  for (let index = 0; index < 4; index += 1) {
    window.scrollBy(0, window.innerHeight);
    await delay(1500);
  }
  const tweets = Array.from(document.querySelectorAll('article[data-testid="tweet"]')).map((article) => {
    const nameEl = article.querySelector('[data-testid="User-Name"]');
    const spans = nameEl ? Array.from(nameEl.querySelectorAll("span")) : [];
    const author = spans[0] ? text(spans[0]) : "";
    const handle = text(spans.find((span) => text(span).startsWith("@")) || null);
    const tweetTextEl = article.querySelector('[data-testid="tweetText"]');
    const timeEl = article.querySelector("time");
    const linkEl = timeEl ? timeEl.closest("a") : null;
    const outboundLinks = tweetTextEl
      ? Array.from(tweetTextEl.querySelectorAll("a[href]"))
          .map((link) => link.href)
          .filter((href) => href && !href.includes("x.com") && !href.includes("twitter.com"))
      : [];
    return { author, handle, text: text(tweetTextEl), url: linkEl ? linkEl.href : "", outboundLinks };
  });
  return { success: true, data: tweets };
}

async function scrapeRedditSaved() {
  const posts = Array.from(document.querySelectorAll(".thing")).map((thing) => {
    const titleEl = thing.querySelector("a.title");
    const bodyEl = thing.querySelector(".usertext-body .md");
    const externalLinks = bodyEl
      ? Array.from(bodyEl.querySelectorAll("a[href]"))
          .map((link) => link.href)
          .filter((href) => href && !href.includes("reddit.com"))
      : [];
    return {
      title: text(titleEl),
      body: text(bodyEl),
      url: titleEl ? titleEl.href : "",
      author: text(thing.querySelector(".author")),
      subreddit: text(thing.querySelector(".subreddit")),
      externalLinks,
    };
  });
  return { success: true, data: posts };
}

async function queryGrok(query) {
  const textarea = document.querySelector("textarea[placeholder]") || document.querySelector("textarea");
  if (!(textarea instanceof HTMLTextAreaElement)) {
    return { success: false, error: "grok textarea not found" };
  }
  textarea.focus();
  textarea.value = query;
  textarea.dispatchEvent(new Event("input", { bubbles: true }));
  await delay(200);
  textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", code: "Enter", keyCode: 13, bubbles: true }));

  const start = Date.now();
  let lastText = "";
  let stableCount = 0;
  while (Date.now() - start < 60_000) {
    await delay(1500);
    const messages = document.querySelectorAll('[class*="message"]');
    const current = messages.length ? text(messages[messages.length - 1] || null) : "";
    if (current && current === lastText) {
      stableCount += 1;
      if (stableCount >= 2) {
        return { success: true, data: { response: current } };
      }
    } else {
      stableCount = 0;
    }
    lastText = current;
  }
  return { success: true, data: { response: lastText, warning: "response may be incomplete (timed out)" } };
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  let work;
  if (msg.type === "extract_page") {
    work = Promise.resolve(pageSnapshot());
  } else if (msg.type === "scrape" && msg.source === "x-bookmarks") {
    work = scrapeXBookmarks();
  } else if (msg.type === "scrape" && msg.source === "reddit-saved") {
    work = scrapeRedditSaved();
  } else if (msg.type === "query" && msg.source === "grok") {
    work = queryGrok(msg.query || "");
  } else {
    sendResponse({ success: false, error: `unhandled: type=${msg.type || ""} source=${msg.source || ""}` });
    return false;
  }

  work
    .then((result) => sendResponse(result))
    .catch((error) => sendResponse({ success: false, error: error instanceof Error ? error.message : String(error) }));
  return true;
});
