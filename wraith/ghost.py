"""WRAITH ghost mode — headless scraping with Safari cookie injection.

Uses cookies extracted from Safari's cookie store (via wraith.cookies) to
authenticate httpx and Playwright WebKit sessions. No stored session files,
no AppleScript — just raw cookie theft and headless browsers.
"""

from __future__ import annotations

import logging
from urllib.parse import urlparse

import httpx
from playwright.async_api import async_playwright, Browser, BrowserContext

from wraith.cookies import extract_cookies, cookies_to_playwright

log = logging.getLogger(__name__)

_USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) "
    "AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
)

_LOGIN_SIGNALS = ("login", "/flow/", "signin", "sign_in", "auth/", "accounts.google")


def _looks_like_login(url: str) -> bool:
    lower = url.lower()
    return any(sig in lower for sig in _LOGIN_SIGNALS)


class GhostSession:
    """Headless session that rides Safari's cookies into authenticated pages."""

    def __init__(self, domains: list[str]) -> None:
        self._domains = domains
        self._cookies = extract_cookies(domains)
        self._browser: Browser | None = None
        self._context: BrowserContext | None = None
        self._pw = None

        if not self._cookies:
            log.warning("ghost: no cookies extracted for domains %s", domains)
        else:
            log.info("ghost: extracted %d cookies across %s", len(self._cookies), domains)

    async def fetch(self, url: str, needs_js: bool = False) -> dict:
        """Fetch a URL, returning {"url", "content", "title"}.

        needs_js=False  → httpx with cookie header
        needs_js=True   → Playwright WebKit with injected cookies
        """
        if needs_js:
            return await self._fetch_js(url)
        return await self._fetch_plain(url)

    async def _fetch_plain(self, url: str) -> dict:
        domain = urlparse(url).netloc
        # Build cookie header from extracted cookies matching this domain
        cookie_pairs = []
        for key, val in self._cookies.items():
            cookie_pairs.append(f"{key}={val}")
        cookie_header = "; ".join(cookie_pairs)

        headers = {
            "User-Agent": _USER_AGENT,
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
            "Accept-Language": "en-US,en;q=0.9",
        }
        if cookie_header:
            headers["Cookie"] = cookie_header

        try:
            async with httpx.AsyncClient(
                headers=headers,
                follow_redirects=True,
                timeout=httpx.Timeout(30.0),
            ) as client:
                resp = await client.get(url)

            if _looks_like_login(str(resp.url)):
                log.warning("ghost: auth redirect detected for %s — cookies may be expired", url)
                return {"url": url, "content": "", "title": ""}

            text = resp.text
            # Extract title from HTML
            title = _extract_title(text)
            return {"url": str(resp.url), "content": text, "title": title}

        except httpx.HTTPError as exc:
            log.error("ghost: httpx error fetching %s: %s", url, exc)
            return {"url": url, "content": "", "title": ""}

    async def _fetch_js(self, url: str) -> dict:
        try:
            if not self._browser:
                self._pw = await async_playwright().start()
                self._browser = await self._pw.webkit.launch(headless=True)

            if not self._context:
                self._context = await self._browser.new_context(
                    viewport={"width": 1920, "height": 1080},
                    user_agent=_USER_AGENT,
                )
                # Inject cookies for all domains
                all_pw_cookies = []
                for domain in self._domains:
                    all_pw_cookies.extend(cookies_to_playwright(self._cookies, domain))
                if all_pw_cookies:
                    await self._context.add_cookies(all_pw_cookies)

            page = await self._context.new_page()
            try:
                await page.goto(url, wait_until="domcontentloaded", timeout=45_000)

                if _looks_like_login(page.url):
                    log.warning("ghost: auth redirect detected for %s — cookies may be expired", url)
                    return {"url": url, "content": "", "title": ""}

                title = await page.title()
                content = await page.content()
                return {"url": page.url, "content": content, "title": title}
            finally:
                await page.close()

        except Exception as exc:
            log.error("ghost: Playwright error fetching %s: %s", url, exc)
            return {"url": url, "content": "", "title": ""}

    async def close(self) -> None:
        if self._context:
            await self._context.close()
            self._context = None
        if self._browser:
            await self._browser.close()
            self._browser = None
        if self._pw:
            await self._pw.stop()
            self._pw = None


def _extract_title(html: str) -> str:
    """Quick title extraction without pulling in a full parser."""
    start = html.find("<title")
    if start == -1:
        return ""
    start = html.find(">", start)
    if start == -1:
        return ""
    end = html.find("</title>", start)
    if end == -1:
        return ""
    return html[start + 1 : end].strip()


# ── Specific scrapers ──────────────────────────────────────────────────


async def scrape_x_bookmarks(max_items: int = 40) -> list[dict]:
    """Scrape X/Twitter bookmarks using ghost mode with Safari cookies.

    Returns list of {"tweet_id", "title", "body", "url", "author_name",
    "handle", "outbound_links"}.
    """
    session = GhostSession(domains=["x.com", "twitter.com"])
    if not session._cookies:
        log.warning("ghost: no X cookies available — cannot scrape bookmarks")
        return []

    tweets: list[dict] = []

    try:
        # Spin up browser + context directly
        pw = await async_playwright().start()
        browser = await pw.webkit.launch(headless=True)
        context = await browser.new_context(
            viewport={"width": 1920, "height": 1080},
            user_agent=_USER_AGENT,
        )
        all_pw_cookies = []
        for domain in session._domains:
            all_pw_cookies.extend(cookies_to_playwright(session._cookies, domain))
        if all_pw_cookies:
            await context.add_cookies(all_pw_cookies)

        # Store on session for enrichment phase
        session._pw = pw
        session._browser = browser
        session._context = context

        page = await context.new_page()
        try:
            await page.goto("https://x.com/i/bookmarks", wait_until="domcontentloaded", timeout=45_000)

            if _looks_like_login(page.url):
                log.warning("ghost: X session expired — login redirect at %s", page.url)
                return []

            # Wait for tweets to render (React hydration)
            try:
                await page.wait_for_selector('article[data-testid="tweet"]', timeout=15_000)
            except Exception:
                log.warning("ghost: X bookmarks — no tweet elements appeared within 15s")
                return []

            # Extra settle time for lazy content
            await page.wait_for_timeout(2000)

            # X virtualizes the DOM — scrolled-past tweets lose their content.
            # Extract tweets in JS land in one shot per scroll position.
            seen_urls: set[str] = set()

            for scroll_pass in range(5):  # 5 passes: initial + 4 scrolls
                batch = await page.evaluate('''() => {
                    const articles = document.querySelectorAll('article[data-testid="tweet"]');
                    return Array.from(articles).map(article => {
                        const textEl = article.querySelector('[data-testid="tweetText"]');
                        const body = textEl ? textEl.textContent.trim() : '';

                        // URL from timestamp link
                        let url = '';
                        const timeEl = article.querySelector('time');
                        if (timeEl) {
                            const a = timeEl.closest('a');
                            if (a) url = a.href;
                        }
                        if (!url) {
                            const statusLink = article.querySelector('a[href*="/status/"]');
                            if (statusLink) {
                                const href = statusLink.getAttribute('href') || '';
                                url = href.startsWith('/') ? 'https://x.com' + href : href;
                            }
                        }

                        // Author
                        const userEl = article.querySelector('[data-testid="User-Name"]');
                        const userText = userEl ? userEl.textContent : '';
                        const parts = userText.split('\\n').map(s => s.trim()).filter(Boolean);
                        const authorName = parts[0] || '';
                        const handle = parts.find(p => p.startsWith('@')) || '';

                        // Outbound links
                        const outbound = [];
                        if (textEl) {
                            for (const a of textEl.querySelectorAll('a[href]')) {
                                const h = a.href;
                                if (h && !h.includes('x.com') && !h.includes('twitter.com') && h.startsWith('http')) {
                                    if (!outbound.includes(h)) outbound.push(h);
                                }
                            }
                        }

                        return { body, url, authorName, handle, outbound: outbound.slice(0, 3) };
                    });
                }''')

                for item in batch:
                    url = item.get("url", "")
                    if not url or url in seen_urls:
                        continue
                    seen_urls.add(url)

                    body = item.get("body", "")
                    tweet_id = url.split("/status/")[1].split("?")[0] if "/status/" in url else ""
                    title = body.split("\n")[0][:120] if body else "X bookmark"

                    tweets.append({
                        "tweet_id": tweet_id,
                        "title": title,
                        "body": body,
                        "url": url,
                        "author_name": item.get("authorName", ""),
                        "handle": item.get("handle", ""),
                        "outbound_links": item.get("outbound", []),
                    })

                    if len(tweets) >= max_items:
                        break

                if len(tweets) >= max_items:
                    break

                # Scroll for next batch
                await page.mouse.wheel(0, 2000)
                await page.wait_for_timeout(1500)

            log.info("ghost: X bookmarks — found %d tweets across %d scroll passes", len(tweets), scroll_pass + 1)

        finally:
            await page.close()

        # Enrich: open each tweet for full thread + fetch linked articles
        log.info("ghost: enriching %d tweets — opening each for full content", len(tweets))
        for idx, tweet in enumerate(tweets):
            tweet_url = tweet.get("url", "")
            if not tweet_url:
                continue

            # Open the tweet page to get full text + thread + replies
            try:
                page = await context.new_page()
                try:
                    await page.goto(tweet_url, wait_until="domcontentloaded", timeout=30_000)
                    try:
                        await page.wait_for_selector('[data-testid="tweetText"]', timeout=10_000)
                    except Exception:
                        log.debug("ghost: no tweetText on thread page for %s", tweet.get("tweet_id"))
                        continue
                    await page.wait_for_timeout(2000)

                    # Extract all text from the thread page via JS (avoid virtualization)
                    thread_data = await page.evaluate('''() => {
                        const texts = document.querySelectorAll('[data-testid="tweetText"]');
                        return Array.from(texts).slice(0, 15).map(el => el.textContent.trim());
                    }''')

                    if thread_data:
                        # First element is the main tweet — update body if we got more
                        full_body = thread_data[0]
                        if len(full_body) > len(tweet.get("body", "")):
                            tweet["body"] = full_body
                            tweet["title"] = full_body.split("\n")[0][:120]

                        # Remaining are replies/thread
                        replies = [t for t in thread_data[1:] if t]
                        if replies:
                            tweet["thread"] = replies

                    # Also grab quote tweet content if present
                    quote = await page.evaluate('''() => {
                        const qt = document.querySelector('[data-testid="quoteTweet"]');
                        if (!qt) return null;
                        const textEl = qt.querySelector('[data-testid="tweetText"]');
                        return textEl ? textEl.textContent.trim() : null;
                    }''')
                    if quote:
                        tweet["quote_tweet"] = quote

                    log.info("ghost: enriched [%d/%d] %s — %d chars, %d replies",
                             idx + 1, len(tweets), tweet.get("tweet_id", "?"),
                             len(tweet.get("body", "")),
                             len(tweet.get("thread", [])))

                finally:
                    await page.close()
            except Exception as exc:
                log.debug("ghost: failed to enrich tweet %s: %s", tweet.get("tweet_id"), exc)

            # Fetch linked articles via httpx
            outbound = tweet.get("outbound_links", [])
            for link in outbound[:2]:
                try:
                    result = await session.fetch(link, needs_js=False)
                    content = result.get("content", "")
                    title = result.get("title", "")
                    if content and len(content) > 200:
                        import re
                        text = re.sub(r'<[^>]+>', ' ', content)
                        text = re.sub(r'\s+', ' ', text).strip()
                        if len(text) > 200:
                            truncated = text[:3000]
                            if len(text) > 3000:
                                truncated += "\n[... truncated]"
                            if "linked_articles" not in tweet:
                                tweet["linked_articles"] = []
                            tweet["linked_articles"].append({"title": title, "url": link, "content": truncated})
                            log.info("ghost: fetched linked article from %s (%d chars)", link, len(text))
                except Exception as exc:
                    log.debug("ghost: failed to fetch article %s: %s", link, exc)

    except Exception as exc:
        log.error("ghost: X bookmarks scrape failed: %s", exc)
        return []
    finally:
        await session.close()

    log.info("ghost: X bookmarks — extracted %d tweets (enriched)", len(tweets))
    return tweets


async def scrape_reddit_saved(max_items: int = 40) -> list[dict]:
    """Scrape Reddit saved posts using ghost mode with Safari cookies.

    Opens each post individually to extract full content, comments, and
    linked articles — not just listing-page titles.

    Returns list of {"post_id", "title", "body", "url", "author",
    "subreddit", "external_url"}.
    """
    session = GhostSession(domains=["reddit.com", "old.reddit.com"])
    if not session._cookies:
        log.warning("ghost: no Reddit cookies available — cannot scrape saved")
        return []

    posts: list[dict] = []
    saved_url = "https://old.reddit.com/user/me/saved/"

    try:
        # Use JS mode — old.reddit blocks plain httpx
        posts = await _parse_reddit_with_js(session, saved_url, max_items)
        if not posts:
            log.warning("ghost: Reddit JS parse returned no posts")
            return []

        # Enrich: open each post for full content + comments
        log.info("ghost: enriching %d Reddit posts — opening each for full content", len(posts))

        # Ensure we have a browser context for enrichment
        if not session._context:
            # Force context creation
            await session.fetch(saved_url, needs_js=True)

        if session._context:
            for idx, post in enumerate(posts):
                post_url = post.get("url", "")
                if not post_url:
                    continue

                try:
                    page = await session._context.new_page()
                    try:
                        await page.goto(post_url, wait_until="domcontentloaded", timeout=30_000)
                        await page.wait_for_timeout(2000)

                        # Extract full post content + top comments via JS
                        data = await page.evaluate('''() => {
                            // Self-text body — target the post's own expando, not sidebar/comments
                            // On old.reddit, the post self-text lives in .expando .usertext-body .md
                            // or in the .linklisting .usertext-body .md (the first one before comments)
                            let body = '';
                            const expando = document.querySelector('.expando .usertext-body .md');
                            if (expando) {
                                body = expando.textContent.trim();
                            } else {
                                // Fallback: first .usertext-body .md that's not inside a .comment
                                const allMd = document.querySelectorAll('.usertext-body .md');
                                for (const el of allMd) {
                                    if (!el.closest('.comment')) {
                                        body = el.textContent.trim();
                                        break;
                                    }
                                }
                            }

                            // Top comments — only from .comment elements
                            const commentEls = document.querySelectorAll('.comment .usertext-body .md');
                            const comments = Array.from(commentEls).slice(0, 10).map(el => el.textContent.trim()).filter(t => t.length > 20);

                            // Post title (in case listing truncated it)
                            const titleEl = document.querySelector('a.title');
                            const title = titleEl ? titleEl.textContent.trim() : '';

                            // External link if it's a link post
                            const linkEl = document.querySelector('a.title');
                            const href = linkEl ? linkEl.href : '';
                            const isExternal = href && !href.includes('reddit.com') && href.startsWith('http');

                            return { body, comments, title, externalUrl: isExternal ? href : null };
                        }''')

                        # Update post with full content
                        if data.get("body") and len(data["body"]) > len(post.get("body", "")):
                            post["body"] = data["body"][:5000]
                        if data.get("title") and len(data["title"]) > len(post.get("title", "")):
                            post["title"] = data["title"][:200]
                        if data.get("comments"):
                            post["top_comments"] = data["comments"]
                        if data.get("externalUrl"):
                            post["external_url"] = data["externalUrl"]

                        log.info("ghost: enriched Reddit [%d/%d] r/%s — %d chars body, %d comments",
                                 idx + 1, len(posts), post.get("subreddit", "?"),
                                 len(post.get("body", "")),
                                 len(post.get("top_comments", [])))

                    finally:
                        await page.close()
                except Exception as exc:
                    log.debug("ghost: failed to enrich Reddit post %s: %s", post.get("post_id"), exc)

                # Fetch external article if it's a link post
                ext_url = post.get("external_url")
                if ext_url and not post.get("body"):
                    try:
                        result = await session.fetch(ext_url, needs_js=False)
                        content = result.get("content", "")
                        if content and len(content) > 200:
                            import re
                            text = re.sub(r'<[^>]+>', ' ', content)
                            text = re.sub(r'\s+', ' ', text).strip()
                            if len(text) > 200:
                                truncated = text[:3000]
                                if len(text) > 3000:
                                    truncated += "\n[... truncated]"
                                post["linked_article"] = {
                                    "title": result.get("title", ""),
                                    "url": ext_url,
                                    "content": truncated,
                                }
                                log.info("ghost: fetched linked article from %s (%d chars)", ext_url, len(text))
                    except Exception as exc:
                        log.debug("ghost: failed to fetch article %s: %s", ext_url, exc)

    except Exception as exc:
        log.error("ghost: Reddit saved scrape failed: %s", exc)
        return []
    finally:
        await session.close()

    log.info("ghost: Reddit saved — extracted %d posts (enriched)", len(posts))
    return posts


# ── Dispatcher ────────────────────────────────────────────────────

async def _hf_ingest(max_items: int = 40) -> list[dict]:
    """Thin wrapper — Hugging Face ingestion is API-based, no ghost session needed."""
    from wraith.huggingface import ingest_huggingface
    return await ingest_huggingface(max_items=max_items)


_SCRAPERS = {
    "x-bookmarks": scrape_x_bookmarks,
    "reddit-saved": scrape_reddit_saved,
    "huggingface": _hf_ingest,
}


async def ghost_ingest(source: str, max_items: int = 40) -> list[dict]:
    """Dispatch to the appropriate ghost scraper by source name."""
    scraper = _SCRAPERS.get(source)
    if not scraper:
        return [{"error": f"No ghost scraper for source: {source}"}]
    return await scraper(max_items=max_items)


async def _parse_reddit_with_js(
    session: GhostSession, url: str, max_items: int
) -> list[dict]:
    """Use Playwright to parse old.reddit DOM."""
    posts: list[dict] = []

    # Ensure we have a browser context
    if not session._browser:
        # Force a JS fetch to spin up the browser
        await session.fetch(url, needs_js=True)

    if not session._context:
        return []

    page = await session._context.new_page()
    try:
        await page.goto(url, wait_until="domcontentloaded", timeout=45_000)

        if _looks_like_login(page.url):
            log.warning("ghost: Reddit JS mode — login redirect at %s", page.url)
            return []

        await page.wait_for_timeout(2000)

        things = await page.query_selector_all(".thing")
        log.info("ghost: Reddit saved — found %d post elements", len(things))

        seen_urls: set[str] = set()

        for thing in things[:max_items]:
            try:
                title_el = await thing.query_selector("a.title")
                title = (await title_el.inner_text()).strip() if title_el else ""
                title_href = (await title_el.get_attribute("href")) if title_el else ""

                comment_el = await thing.query_selector("a.bylink.comments")
                comment_url = (await comment_el.get_attribute("href")) if comment_el else ""
                post_url = comment_url or title_href or ""

                if not post_url.startswith("http"):
                    post_url = f"https://old.reddit.com{post_url}" if post_url.startswith("/") else ""

                if not post_url or post_url in seen_urls:
                    continue
                seen_urls.add(post_url)

                post_id = ""
                if "/comments/" in post_url:
                    post_id = post_url.split("/comments/")[1].split("/")[0]

                body_el = await thing.query_selector(".md, .usertext-body")
                body = (await body_el.inner_text()).strip() if body_el else ""

                author_el = await thing.query_selector(".author")
                author = (await author_el.inner_text()).strip() if author_el else ""

                sub_el = await thing.query_selector(".subreddit")
                subreddit = (await sub_el.inner_text()).strip() if sub_el else ""
                # Strip leading "r/" — we store just the subreddit name
                if subreddit.startswith("r/"):
                    subreddit = subreddit[2:]

                external_url = None
                if title_href and "reddit.com" not in title_href and title_href.startswith("http"):
                    external_url = title_href

                posts.append({
                    "post_id": post_id,
                    "title": title[:200],
                    "body": body[:2000],
                    "url": post_url,
                    "author": author,
                    "subreddit": subreddit,
                    "external_url": external_url,
                })
            except Exception as exc:
                log.debug("ghost: error parsing Reddit post: %s", exc)

    finally:
        await page.close()

    return posts


def _parse_reddit_html(html: str, max_items: int) -> list[dict]:
    """Fallback: parse old.reddit HTML without a browser.

    Uses basic string parsing — not beautiful, but functional when
    Playwright isn't available or the plain fetch succeeded.
    """
    posts: list[dict] = []

    # old.reddit wraps each post in <div class="thing" ...>
    # This is a rough fallback — the JS parser above is preferred
    try:
        from html.parser import HTMLParser
    except ImportError:
        return []

    # Simple extraction: find thing blocks by data-fullname
    chunks = html.split('data-fullname="')
    for chunk in chunks[1 : max_items + 1]:
        try:
            fullname = chunk.split('"')[0]
            post_id = fullname.replace("t3_", "").replace("t1_", "")

            # Title: look for class="title" ... <a ...>text</a>
            title = ""
            title_start = chunk.find('class="title')
            if title_start != -1:
                a_start = chunk.find(">", title_start + 20)
                # Find the inner <a> tag
                inner_a = chunk.find("<a", title_start)
                if inner_a != -1:
                    text_start = chunk.find(">", inner_a) + 1
                    text_end = chunk.find("</a>", text_start)
                    if text_start > 0 and text_end > text_start:
                        title = chunk[text_start:text_end].strip()[:200]

            # URL: comment permalink
            url = ""
            perm = chunk.find('class="bylink comments')
            if perm != -1:
                href_start = chunk.rfind('href="', max(0, perm - 200), perm)
                if href_start != -1:
                    href_end = chunk.find('"', href_start + 6)
                    url = chunk[href_start + 6 : href_end]
                    if url.startswith("/"):
                        url = f"https://old.reddit.com{url}"

            if not url:
                continue

            posts.append({
                "post_id": post_id,
                "title": title or "Reddit saved",
                "body": "",
                "url": url,
                "author": "",
                "subreddit": "",
                "external_url": None,
            })
        except Exception:
            continue

    return posts
