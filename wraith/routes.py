"""WRAITH FastAPI routes.

Include this router in the main MODUS server to expose WRAITH endpoints
under ``/wraith``.
"""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel

from wraith.core import SOURCES, wraith

router = APIRouter(prefix="/wraith", tags=["wraith"])


# ------------------------------------------------------------------
# Request models
# ------------------------------------------------------------------

class IngestRequest(BaseModel):
    source: str
    max_items: int = 40


class QueryRequest(BaseModel):
    source: str
    query: str


# ------------------------------------------------------------------
# Endpoints
# ------------------------------------------------------------------

@router.get("/status")
async def get_status() -> dict:
    """Current WRAITH mode and connectivity."""
    return await wraith.status()


@router.post("/ingest")
async def post_ingest(req: IngestRequest) -> dict:
    """Ingest items from a source (bridge or ghost)."""
    items = await wraith.ingest(req.source, req.max_items)
    result = {"source": req.source, "mode": wraith.mode, "items": items, "count": len(items)}
    if wraith.last_ingestion:
        result["warnings"] = wraith.last_ingestion.get("warnings", [])
    return result


@router.post("/query")
async def post_query(req: QueryRequest) -> dict:
    """Send a live query via the bridge."""
    return await wraith.query(req.source, req.query)


@router.get("/last_ingestion")
async def get_last_ingestion() -> dict:
    """Last ingestion result — time, source, count, mode, warnings."""
    if wraith.last_ingestion is None:
        return {"status": "no_ingestion_yet"}
    return wraith.last_ingestion


@router.get("/sources")
async def get_sources() -> list[dict]:
    """Available WRAITH sources and their supported modes."""
    return SOURCES


# ------------------------------------------------------------------
# Bridge commands (direct access to the Safari extension)
# ------------------------------------------------------------------

@router.get("/bridge/tabs")
async def bridge_get_tabs() -> dict:
    """Get all open Safari tabs via the bridge extension."""
    from wraith.bridge import bridge
    if not bridge.connected:
        return {"ok": False, "error": "Bridge not connected"}
    try:
        tabs = await bridge.command("get_tabs")
        return {"ok": True, "tabs": tabs}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


class NavigateRequest(BaseModel):
    url: str
    tab_id: int | None = None


@router.post("/bridge/navigate")
async def bridge_navigate(req: NavigateRequest) -> dict:
    """Navigate a Safari tab to a URL via the bridge extension."""
    from wraith.bridge import bridge
    if not bridge.connected:
        return {"ok": False, "error": "Bridge not connected"}
    try:
        result = await bridge.command("navigate", {"url": req.url, "tabId": req.tab_id})
        return {"ok": True, "result": result}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


class ExtractRequest(BaseModel):
    tab_id: int | None = None


@router.post("/bridge/extract")
async def bridge_extract(req: ExtractRequest) -> dict:
    """Extract page content from a Safari tab via the bridge extension."""
    from wraith.bridge import bridge
    if not bridge.connected:
        return {"ok": False, "error": "Bridge not connected"}
    try:
        result = await bridge.command("extract_page", {"tabId": req.tab_id})
        return {"ok": True, "page": result}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


@router.get("/bridge/ping")
async def bridge_ping() -> dict:
    """Ping the bridge extension."""
    from wraith.bridge import bridge
    if not bridge.connected:
        return {"ok": False, "error": "Bridge not connected"}
    try:
        result = await bridge.command("ping")
        return {"ok": True, "result": result}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}
