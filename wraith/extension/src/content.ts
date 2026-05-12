import type { PageSnapshot, SourceResult } from "./types";

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function text(el: Element | null): string {
  return el?.textContent?.replace(/\s+/g, " ").trim() ?? "";
}

function pageSnapshot(): PageSnapshot {
  const selection = window.getSelection()?.toString().trim() ?? "";
  const description = document.querySelector('meta[name="description"], meta[property="og:description"]')?.getAttribute("content")?.trim() ?? "";
  const body = document.body?.innerText?.replace(/\s+/g, " ").trim() ?? "";
  return {
    title: document.title || window.location.href,
    url: window.location.href,
    text: body.slice(0, 12000),
    selection,
    description,
  };
}

async function scrapeXBookmarks(): Promise<SourceResult> {
  for (let index = 0; index < 4; index += 1) {
    window.scrollBy(0, window.innerHeight);
    await delay(1500);
  }
  const tweets = Array.from(document.querySelectorAll('article[data-testid="tweet"]')).map((article) => {
    const nameEl = article.querySelector('[data-testid="User-Name"]');
    const spans = nameEl ? Array.from(nameEl.querySelectorAll("span")) : [];
    const author = spans[0] ? text(spans[0]) : "";
    const handle = text(spans.find((span) => text(span).startsWith("@")) ?? null);
    const tweetTextEl = article.querySelector('[data-testid="tweetText"]');
    const timeEl = article.querySelector("time");
    const linkEl = timeEl?.closest("a");
    const outboundLinks = tweetTextEl
      ? Array.from(tweetTextEl.querySelectorAll("a[href]"))
          .map((link) => (link as HTMLAnchorElement).href)
          .filter((href) => href && !href.includes("x.com") && !href.includes("twitter.com"))
      : [];
    return { author, handle, text: text(tweetTextEl), url: (linkEl as HTMLAnchorElement | null)?.href ?? "", outboundLinks };
  });
  return { success: true, data: tweets };
}

async function scrapeRedditSaved(): Promise<SourceResult> {
  const posts = Array.from(document.querySelectorAll(".thing")).map((thing) => {
    const titleEl = thing.querySelector("a.title") as HTMLAnchorElement | null;
    const bodyEl = thing.querySelector(".usertext-body .md");
    const externalLinks = bodyEl
      ? Array.from(bodyEl.querySelectorAll("a[href]"))
          .map((link) => (link as HTMLAnchorElement).href)
          .filter((href) => href && !href.includes("reddit.com"))
      : [];
    return {
      title: text(titleEl),
      body: text(bodyEl),
      url: titleEl?.href ?? "",
      author: text(thing.querySelector(".author")),
      subreddit: text(thing.querySelector(".subreddit")),
      externalLinks,
    };
  });
  return { success: true, data: posts };
}

async function queryGrok(query: string): Promise<SourceResult> {
  const textarea = document.querySelector("textarea[placeholder]") ?? document.querySelector("textarea");
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
    const current = messages.length ? text(messages[messages.length - 1] ?? null) : "";
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
  const message = msg as { type?: string; source?: string; query?: string };
  let work: Promise<unknown>;
  if (message.type === "extract_page") {
    work = Promise.resolve(pageSnapshot());
  } else if (message.type === "scrape" && message.source === "x-bookmarks") {
    work = scrapeXBookmarks();
  } else if (message.type === "scrape" && message.source === "reddit-saved") {
    work = scrapeRedditSaved();
  } else if (message.type === "query" && message.source === "grok") {
    work = queryGrok(message.query ?? "");
  } else {
    sendResponse({ success: false, error: `unhandled: type=${message.type ?? ""} source=${message.source ?? ""}` });
    return false;
  }

  work
    .then((result) => sendResponse(result))
    .catch((error: unknown) => sendResponse({ success: false, error: error instanceof Error ? error.message : String(error) }));
  return true;
});
