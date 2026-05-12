"""WRAITH Hugging Face ingestion — API-based trending papers, models, and user likes.

Pure API calls against https://huggingface.co/api — no scraping, no browser,
no cookies. Ghost-mode only (no bridge needed).
"""

from __future__ import annotations

import logging
import os
from typing import Any

import httpx

log = logging.getLogger(__name__)

_BASE = "https://huggingface.co/api"
_TIMEOUT = httpx.Timeout(30.0)

# Tags that signal relevance to the Enclave's local-first ML stack
_RELEVANT_TAGS: frozenset[str] = frozenset({
    "mlx", "apple", "local", "quantized", "gguf", "lora", "qlora",
    "4bit", "8bit", "awq", "gptq", "exl2", "coreml", "ane",
    "on-device", "edge", "small", "tiny", "distil", "pruned",
})


def _hf_headers() -> dict[str, str]:
    """Build request headers — include auth token when available."""
    headers: dict[str, str] = {"Accept": "application/json"}
    token = os.environ.get("HF_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def _is_relevant(tags: list[str], model_id: str) -> bool:
    """Return True if the model looks relevant to local/quantized ML work."""
    lower_tags = {t.lower() for t in tags}
    lower_id = model_id.lower()
    return bool(
        lower_tags & _RELEVANT_TAGS
        or any(kw in lower_id for kw in _RELEVANT_TAGS)
    )


# ── Public API ────────────────────────────────────────────────────


async def fetch_trending_papers(limit: int = 20) -> list[dict[str, Any]]:
    """Fetch daily trending papers from Hugging Face.

    GET /api/daily_papers — returns a list of paper objects with nested
    ``paper`` dicts containing title, summary, authors, and arxiv id.
    """
    papers: list[dict[str, Any]] = []
    try:
        async with httpx.AsyncClient(headers=_hf_headers(), timeout=_TIMEOUT) as client:
            resp = await client.get(f"{_BASE}/daily_papers")
            resp.raise_for_status()
            raw = resp.json()
    except (httpx.HTTPError, ValueError) as exc:
        log.error("huggingface: failed to fetch trending papers: %s", exc)
        return []

    for entry in raw[:limit]:
        paper = entry.get("paper", entry)
        arxiv_id = paper.get("id", "")
        authors_raw = paper.get("authors", [])
        authors = [
            a.get("name", a.get("user", {}).get("fullname", ""))
            for a in authors_raw
            if isinstance(a, dict)
        ]
        papers.append({
            "title": paper.get("title", ""),
            "summary": paper.get("summary", ""),
            "authors": authors,
            "arxiv_url": f"https://arxiv.org/abs/{arxiv_id}" if arxiv_id else "",
            "upvotes": entry.get("paper", {}).get("upvotes", entry.get("upvotes", 0)),
        })

    log.info("huggingface: fetched %d trending papers", len(papers))
    return papers


async def fetch_trending_models(limit: int = 20) -> list[dict[str, Any]]:
    """Fetch trending models from Hugging Face.

    GET /api/models?sort=trending&limit=N — then optionally fetch
    individual model cards for descriptions.
    """
    models: list[dict[str, Any]] = []
    try:
        async with httpx.AsyncClient(headers=_hf_headers(), timeout=_TIMEOUT) as client:
            resp = await client.get(
                f"{_BASE}/models",
                params={"sort": "likes7d", "direction": "-1", "limit": limit},
            )
            resp.raise_for_status()
            raw = resp.json()

            for entry in raw[:limit]:
                model_id: str = entry.get("modelId", entry.get("id", ""))
                tags: list[str] = entry.get("tags", [])
                pipeline_tag: str = entry.get("pipeline_tag", "")

                # Fetch description from model card for relevant models
                description = ""
                if _is_relevant(tags, model_id):
                    try:
                        card_resp = await client.get(f"{_BASE}/models/{model_id}")
                        if card_resp.status_code == 200:
                            card = card_resp.json()
                            description = card.get("cardData", {}).get("description", "")
                            if not description:
                                # Some models store it at top level
                                description = card.get("description", "")
                    except Exception as exc:
                        log.debug("huggingface: failed to fetch card for %s: %s", model_id, exc)

                models.append({
                    "model_id": model_id,
                    "author": entry.get("author", model_id.split("/")[0] if "/" in model_id else ""),
                    "tags": tags,
                    "downloads": entry.get("downloads", 0),
                    "likes": entry.get("likes", 0),
                    "pipeline_tag": pipeline_tag,
                    "description": description,
                    "relevant": _is_relevant(tags, model_id),
                })

    except (httpx.HTTPError, ValueError) as exc:
        log.error("huggingface: failed to fetch trending models: %s", exc)
        return []

    log.info("huggingface: fetched %d trending models (%d relevant)",
             len(models), sum(1 for m in models if m.get("relevant")))
    return models


async def fetch_user_likes(username: str, limit: int = 40) -> list[dict[str, Any]]:
    """Fetch a user's liked models/datasets/spaces.

    GET /api/users/{username}/likes — may require HF_TOKEN for
    private profiles.
    """
    items: list[dict[str, Any]] = []
    try:
        async with httpx.AsyncClient(headers=_hf_headers(), timeout=_TIMEOUT) as client:
            resp = await client.get(f"{_BASE}/users/{username}/likes")
            resp.raise_for_status()
            raw = resp.json()
    except (httpx.HTTPError, ValueError) as exc:
        log.error("huggingface: failed to fetch likes for %s: %s", username, exc)
        return []

    for entry in raw[:limit]:
        items.append({
            "type": entry.get("type", "unknown"),
            "id": entry.get("repo", {}).get("name", entry.get("id", "")),
            "url": f"https://huggingface.co/{entry.get('repo', {}).get('name', '')}",
        })

    log.info("huggingface: fetched %d likes for user %s", len(items), username)
    return items


async def ingest_huggingface(max_items: int = 40) -> list[dict[str, Any]]:
    """Combined ingestion: trending papers + trending models.

    Returns items in the standard WRAITH format:
    {"title", "url", "content", "source": "huggingface", "metadata": {...}}

    Prioritises relevant (local/quantized/MLX) models, then papers.
    """
    half = max_items // 2
    papers = await fetch_trending_papers(limit=half)
    models = await fetch_trending_models(limit=half)

    results: list[dict[str, Any]] = []

    # Models first — relevant ones up top
    relevant_models = [m for m in models if m.get("relevant")]
    other_models = [m for m in models if not m.get("relevant")]

    for model in relevant_models + other_models:
        model_id = model["model_id"]
        tags_str = ", ".join(model.get("tags", [])[:10])
        content_parts = [
            f"Model: {model_id}",
            f"Pipeline: {model.get('pipeline_tag', 'N/A')}",
            f"Tags: {tags_str}" if tags_str else "",
            f"Downloads: {model.get('downloads', 0):,}  Likes: {model.get('likes', 0):,}",
        ]
        if model.get("description"):
            content_parts.append(f"\n{model['description'][:1000]}")

        results.append({
            "title": model_id,
            "url": f"https://huggingface.co/{model_id}",
            "content": "\n".join(p for p in content_parts if p),
            "source": "huggingface",
            "metadata": {
                "type": "model",
                "author": model.get("author", ""),
                "tags": model.get("tags", []),
                "downloads": model.get("downloads", 0),
                "likes": model.get("likes", 0),
                "pipeline_tag": model.get("pipeline_tag", ""),
                "relevant": model.get("relevant", False),
            },
        })

        if len(results) >= max_items:
            break

    # Then papers
    for paper in papers:
        if len(results) >= max_items:
            break

        summary = paper.get("summary", "")
        authors_str = ", ".join(paper.get("authors", [])[:5])
        content_parts = [
            paper.get("title", ""),
            f"Authors: {authors_str}" if authors_str else "",
            f"Upvotes: {paper.get('upvotes', 0)}",
            "",
            summary[:2000] if summary else "",
        ]

        results.append({
            "title": paper.get("title", "Untitled paper"),
            "url": paper.get("arxiv_url", ""),
            "content": "\n".join(p for p in content_parts if p),
            "source": "huggingface",
            "metadata": {
                "type": "paper",
                "authors": paper.get("authors", []),
                "upvotes": paper.get("upvotes", 0),
                "arxiv_url": paper.get("arxiv_url", ""),
            },
        })

    log.info("huggingface: ingested %d items (%d models, %d papers)",
             len(results),
             sum(1 for r in results if r["metadata"]["type"] == "model"),
             sum(1 for r in results if r["metadata"]["type"] == "paper"))
    return results
