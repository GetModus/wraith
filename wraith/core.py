"""WRAITH core orchestrator.

Manages mode selection between Bridge (live WebSocket to Safari extension)
and Ghost (headless scraping via Safari cookies). The singleton ``wraith``
instance is the single point of contact for all WRAITH operations.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from pathlib import Path

log = logging.getLogger(__name__)

_COOKIES_PATH = Path.home() / "Library" / "Containers" / "com.apple.Safari" / "Data" / "Library" / "Cookies" / "Cookies.binarycookies"

# Sources and their supported modes
SOURCES: list[dict] = [
    {"name": "x-bookmarks", "modes": ["bridge", "ghost"]},
    {"name": "reddit-saved", "modes": ["bridge", "ghost"]},
    {"name": "grok", "modes": ["bridge"]},
    {"name": "huggingface", "modes": ["ghost"]},
]

_GHOST_SOURCES = {s["name"] for s in SOURCES if "ghost" in s["modes"]}


class Wraith:
    """Dual-mode browser orchestrator.

    Bridge mode: live WebSocket connection to a Safari extension.
    Ghost mode: headless scraping using Safari's cookie jar.
    """

    def __init__(self) -> None:
        self._bridge_connected: bool = False
        self.last_ingestion: dict | None = None

    # ------------------------------------------------------------------
    # Mode
    # ------------------------------------------------------------------

    @property
    def mode(self) -> str:
        """Current operating mode — 'bridge' or 'ghost'."""
        return "bridge" if self._bridge_connected else "ghost"

    def bridge_connected(self, connected: bool) -> None:
        """Called by the WebSocket handler when a bridge connects/disconnects."""
        prev = self._bridge_connected
        self._bridge_connected = connected
        if connected and not prev:
            log.info("Bridge connected — switching to bridge mode")
        elif not connected and prev:
            log.info("Bridge disconnected — falling back to ghost mode")

    # ------------------------------------------------------------------
    # Ingest
    # ------------------------------------------------------------------

    async def ingest(self, source: str, max_items: int = 40) -> list[dict]:
        """Ingest items from *source*.

        In bridge mode, attempts a live scrape via the WebSocket extension.
        Falls through to ghost mode if the bridge stub hasn't been wired yet.
        In ghost mode, delegates to the appropriate ghost scraper.
        """
        if source not in {s["name"] for s in SOURCES}:
            return [{"error": f"Unknown source: {source}"}]

        # Bridge path (stub — falls through to ghost for now)
        if self._bridge_connected:
            log.info("Bridge connected but live ingest not yet wired — falling through to ghost for %s", source)

        # Ghost path
        if source not in _GHOST_SOURCES:
            return [{"error": f"Source '{source}' requires bridge mode (no ghost scraper available)"}]

        items = await self._ghost_ingest(source, max_items)
        self._record_ingestion(source, items)
        return items

    async def _ghost_ingest(self, source: str, max_items: int) -> list[dict]:
        """Delegate to the ghost scraper for *source*."""
        try:
            from wraith.ghost import ghost_ingest
            return await ghost_ingest(source, max_items)
        except ImportError:
            log.warning("wraith.ghost module not available — ghost ingest unavailable")
            return [{"error": "Ghost module not installed"}]
        except Exception as exc:
            log.exception("Ghost ingest failed for %s", source)
            return [{"error": f"Ghost ingest failed: {exc}"}]

    def _record_ingestion(self, source: str, items: list[dict]) -> None:
        """Track the last ingestion result and send notifications."""
        errors = [i.get("error") for i in items if "error" in i]
        clean_count = len([i for i in items if "error" not in i])
        warnings: list[str] = []

        if errors:
            warnings.extend(errors[:3])
        if clean_count == 0 and not errors:
            warnings.append(f"{source} returned 0 items — cookies may be expired")

        self.last_ingestion = {
            "time": datetime.now(timezone.utc).isoformat(),
            "source": source,
            "count": clean_count,
            "mode": self.mode,
            "warnings": warnings,
        }

        # Fire-and-forget notification
        try:
            from wraith.notify import notify_ingestion
            notify_ingestion(source, clean_count, self.mode, warnings or None)
        except Exception as exc:
            log.warning("Ingestion notification failed: %s", exc)

    # ------------------------------------------------------------------
    # Query
    # ------------------------------------------------------------------

    async def query(self, source: str, query: str) -> dict:
        """Send a live query via the bridge.

        Queries require an active WebSocket connection — there is no ghost
        fallback for interactive queries.
        """
        if not self._bridge_connected:
            return {"error": "Bridge not connected, query requires live session"}

        # Stub — will be wired to actual bridge message passing
        log.info("Query stub called: source=%s query=%s", source, query)
        return {"error": "Bridge query not yet implemented"}

    # ------------------------------------------------------------------
    # Status
    # ------------------------------------------------------------------

    async def status(self) -> dict:
        """Current WRAITH status."""
        ghost_available = _COOKIES_PATH.exists()
        result = {
            "mode": self.mode,
            "bridge_connected": self._bridge_connected,
            "ghost_available": ghost_available,
        }
        if self.last_ingestion:
            result["last_ingestion"] = self.last_ingestion
        return result


# Singleton
wraith = Wraith()
