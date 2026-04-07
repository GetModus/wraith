(function () {
  "use strict";

  var ext = typeof browser !== "undefined" ? browser : (typeof chrome !== "undefined" ? chrome : null);

  if (!ext || !ext.runtime || !ext.runtime.onMessage) {
    return;
  }

  // --- Generic Page Extraction ---

  function getMeta(names) {
    var i, selector, element;
    for (i = 0; i < names.length; i += 1) {
      selector = 'meta[name="' + names[i] + '"], meta[property="' + names[i] + '"]';
      element = document.querySelector(selector);
      if (element && element.getAttribute("content")) {
        return element.getAttribute("content");
      }
    }
    return "";
  }

  function normalizeText(value, maxLength) {
    if (!value) { return ""; }
    value = String(value).replace(/\s+/g, " ").trim();
    if (maxLength && value.length > maxLength) {
      return value.slice(0, maxLength);
    }
    return value;
  }

  function collectHeadings() {
    var nodes = document.querySelectorAll("h1, h2, h3, h4, h5, h6");
    var result = [];
    var i, text;
    for (i = 0; i < nodes.length && result.length < 100; i += 1) {
      text = normalizeText(nodes[i].textContent, 500);
      if (text) {
        result.push({ level: parseInt(nodes[i].tagName.charAt(1), 10), text: text });
      }
    }
    return result;
  }

  function collectLinks() {
    var nodes = document.querySelectorAll("a[href]");
    var result = [];
    var i, href, text;
    for (i = 0; i < nodes.length && result.length < 300; i += 1) {
      href = nodes[i].href || "";
      text = normalizeText(nodes[i].textContent, 200);
      if (!href || !text) { continue; }
      if (href.indexOf("javascript:") === 0 || href.indexOf("mailto:") === 0) { continue; }
      result.push({ text: text, href: href });
    }
    return result;
  }

  function collectImages() {
    var nodes = document.querySelectorAll("img[src]");
    var result = [];
    var i, node;
    for (i = 0; i < nodes.length && result.length < 50; i += 1) {
      node = nodes[i];
      if ((node.naturalWidth || 0) < 100 || (node.naturalHeight || 0) < 100) { continue; }
      result.push({
        src: node.src,
        alt: normalizeText(node.alt, 200),
        width: node.naturalWidth || 0,
        height: node.naturalHeight || 0
      });
    }
    return result;
  }

  function extractBodyText() {
    if (!document.body) { return ""; }
    var clone = document.body.cloneNode(true);
    var noise = clone.querySelectorAll("script, style, noscript, nav, footer, header, aside, iframe, svg, canvas");
    var i;
    for (i = 0; i < noise.length; i += 1) {
      if (noise[i] && noise[i].parentNode) {
        noise[i].parentNode.removeChild(noise[i]);
      }
    }
    return normalizeText(clone.textContent || "", 50000);
  }

  // --- X/Twitter-Specific Extraction ---

  function isXDomain() {
    var host = window.location.hostname;
    return host === "x.com" || host === "twitter.com" || host === "mobile.twitter.com";
  }

  function extractTweet() {
    if (!isXDomain()) { return null; }

    // Single tweet page: /username/status/1234567890
    var statusMatch = window.location.pathname.match(/^\/([^\/]+)\/status\/(\d+)/);

    if (statusMatch) {
      return extractSingleTweet(statusMatch[1], statusMatch[2]);
    }

    // Bookmarks page: /i/bookmarks
    if (window.location.pathname === "/i/bookmarks") {
      return extractBookmarksPage();
    }

    return null;
  }

  function extractSingleTweet(handle, tweetId) {
    // X renders tweets in article elements
    var articles = document.querySelectorAll('article[data-testid="tweet"]');
    if (articles.length === 0) { return null; }

    // First article is the main tweet on a status page
    var main = articles[0];
    var result = parseTweetArticle(main);
    result.tweet_id = tweetId;
    result.handle = "@" + handle;

    // Collect thread replies (remaining articles)
    var threadTexts = [];
    var i;
    for (i = 1; i < articles.length && i <= 20; i += 1) {
      var reply = parseTweetArticle(articles[i]);
      if (reply.text) {
        threadTexts.push(reply.author + ": " + reply.text);
      }
    }
    result.thread_texts = threadTexts;

    return result;
  }

  function parseTweetArticle(article) {
    var result = {
      author: "",
      handle: "",
      text: "",
      timestamp: "",
      likes: "",
      retweets: "",
      replies: "",
      quoted_text: "",
      quoted_author: "",
      media_urls: [],
      thread_texts: []
    };

    // Author name — first span in the user name container
    var userNameEl = article.querySelector('[data-testid="User-Name"]');
    if (userNameEl) {
      var spans = userNameEl.querySelectorAll("span");
      if (spans.length > 0) {
        result.author = normalizeText(spans[0].textContent, 100);
      }
      // Handle — look for the @mention
      var links = userNameEl.querySelectorAll("a");
      var j;
      for (j = 0; j < links.length; j += 1) {
        var linkText = normalizeText(links[j].textContent, 50);
        if (linkText.charAt(0) === "@") {
          result.handle = linkText;
          break;
        }
      }
    }

    // Tweet text
    var tweetTextEl = article.querySelector('[data-testid="tweetText"]');
    if (tweetTextEl) {
      result.text = normalizeText(tweetTextEl.textContent, 5000);
    }

    // Timestamp
    var timeEl = article.querySelector("time");
    if (timeEl) {
      result.timestamp = timeEl.getAttribute("datetime") || timeEl.textContent || "";
    }

    // Engagement metrics
    var replyBtn = article.querySelector('[data-testid="reply"]');
    var retweetBtn = article.querySelector('[data-testid="retweet"]');
    var likeBtn = article.querySelector('[data-testid="like"], [data-testid="unlike"]');

    if (replyBtn) { result.replies = normalizeText(replyBtn.getAttribute("aria-label"), 50); }
    if (retweetBtn) { result.retweets = normalizeText(retweetBtn.getAttribute("aria-label"), 50); }
    if (likeBtn) { result.likes = normalizeText(likeBtn.getAttribute("aria-label"), 50); }

    // Media (images and videos)
    var mediaImages = article.querySelectorAll('[data-testid="tweetPhoto"] img');
    var k;
    for (k = 0; k < mediaImages.length; k += 1) {
      if (mediaImages[k].src) {
        result.media_urls.push(mediaImages[k].src);
      }
    }

    var mediaVideos = article.querySelectorAll("video");
    for (k = 0; k < mediaVideos.length; k += 1) {
      if (mediaVideos[k].src) {
        result.media_urls.push(mediaVideos[k].src);
      }
    }

    // Quoted tweet
    var quotedEl = article.querySelector('[data-testid="quoteTweet"]') ||
                   article.querySelector('[role="link"][tabindex="0"]');
    if (quotedEl) {
      var quotedTextEl = quotedEl.querySelector('[data-testid="tweetText"]');
      if (quotedTextEl) {
        result.quoted_text = normalizeText(quotedTextEl.textContent, 2000);
      }
      var quotedUserEl = quotedEl.querySelector('[data-testid="User-Name"]');
      if (quotedUserEl) {
        result.quoted_author = normalizeText(quotedUserEl.textContent, 100);
      }
    }

    return result;
  }

  function extractBookmarksPage() {
    // Returns a batch capture — all visible bookmarked tweets
    var articles = document.querySelectorAll('article[data-testid="tweet"]');
    if (articles.length === 0) { return null; }

    var result = {
      author: "bookmarks",
      handle: "@bookmarks",
      text: articles.length + " bookmarked tweets visible",
      tweet_id: "",
      timestamp: new Date().toISOString(),
      likes: "",
      retweets: "",
      replies: "",
      media_urls: [],
      thread_texts: [],
      _bookmarks: []
    };

    var i;
    for (i = 0; i < articles.length; i += 1) {
      var tweet = parseTweetArticle(articles[i]);
      // Try to extract tweet ID from links
      var statusLinks = articles[i].querySelectorAll('a[href*="/status/"]');
      var j;
      for (j = 0; j < statusLinks.length; j += 1) {
        var m = statusLinks[j].href.match(/\/status\/(\d+)/);
        if (m) {
          tweet.tweet_id = m[1];
          break;
        }
      }
      result._bookmarks.push(tweet);
    }

    return result;
  }

  // --- Main Extraction ---

  function extractPage() {
    var bodyText = extractBodyText();
    var words = bodyText ? bodyText.split(/\s+/) : [];
    var tweet = extractTweet();

    var result = {
      url: window.location.href,
      title: document.title || "",
      description: getMeta(["description", "og:description", "twitter:description"]),
      author: getMeta(["author", "article:author"]),
      siteName: getMeta(["og:site_name"]),
      publishedTime: getMeta(["article:published_time", "og:published_time"]),
      ogImage: getMeta(["og:image", "twitter:image"]),
      headings: collectHeadings(),
      links: collectLinks(),
      images: collectImages(),
      bodyText: bodyText,
      wordCount: words.length,
      extractedAt: new Date().toISOString()
    };

    if (tweet) {
      result.tweet = tweet;
    }

    // Copy some meta into the expected fields
    if (result.author === "" && tweet) {
      result.author = tweet.author;
    }

    return result;
  }

  // --- Message Handler ---

  ext.runtime.onMessage.addListener(function (message, sender, sendResponse) {
    if (!message || !message.type) {
      return;
    }

    if (message.type === "extract_page") {
      sendResponse(extractPage());
      return;
    }

    if (message.type === "ping") {
      sendResponse({
        ok: true,
        url: window.location.href,
        title: document.title || "",
        isX: isXDomain()
      });
    }

    if (message.type === "extract_bookmarks") {
      // Explicit bookmark extraction request
      if (!isXDomain() || window.location.pathname !== "/i/bookmarks") {
        sendResponse({ ok: false, error: "Not on X bookmarks page" });
        return;
      }
      var bookmarks = extractBookmarksPage();
      sendResponse({ ok: true, bookmarks: bookmarks });
    }

    if (message.type === "scroll_down") {
      window.scrollBy(0, window.innerHeight * 2);
      sendResponse({ ok: true, scrollY: window.scrollY });
    }
  });
})();
