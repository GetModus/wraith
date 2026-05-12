"""WRAITH Bridge — WebSocket relay to the Safari extension.

The bridge maintains a single WebSocket connection to the MODUS Bridge
Safari extension. Commands are sent to the extension and responses are
correlated by ID using asyncio.Future objects. This enables any part of
MODUS to call ``bridge.command("get_tabs")`` and await the result.
"""

from __future__ import annotations

import asyncio
import json
import logging
import uuid

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from wraith.core import wraith

log = logging.getLogger(__name__)

COMMAND_TIMEOUT = 10  # seconds


class BridgeConnection:
    """Manages a single WebSocket bridge session with command/response relay."""

    def __init__(self) -> None:
        self.ws: WebSocket | None = None
        self._pending: dict[str, asyncio.Future] = {}

    @property
    def connected(self) -> bool:
        return self.ws is not None

    async def connect(self, websocket: WebSocket) -> None:
        """Accept and hold the bridge connection."""
        await websocket.accept()
        self.ws = websocket
        wraith.bridge_connected(True)
        log.info("Bridge WebSocket accepted")

    async def disconnect(self) -> None:
        """Clean up on disconnect."""
        self.ws = None
        wraith.bridge_connected(False)
        # Cancel any pending futures
        for cmd_id, fut in self._pending.items():
            if not fut.done():
                fut.set_exception(ConnectionError("Bridge disconnected"))
        self._pending.clear()
        log.info("Bridge WebSocket closed")

    async def command(self, cmd: str, params: dict | None = None, timeout: float = COMMAND_TIMEOUT) -> dict:
        """Send a command to the extension and await the response.

        Returns the result dict on success. Raises on timeout or error.
        """
        if not self.ws:
            raise ConnectionError("Bridge not connected")

        cmd_id = str(uuid.uuid4())[:8]
        fut: asyncio.Future = asyncio.get_event_loop().create_future()
        self._pending[cmd_id] = fut

        try:
            await self.ws.send_json({
                "id": cmd_id,
                "command": cmd,
                "params": params or {},
            })
            result = await asyncio.wait_for(fut, timeout=timeout)
            return result
        except asyncio.TimeoutError:
            raise TimeoutError(f"Bridge command '{cmd}' timed out after {timeout}s")
        finally:
            self._pending.pop(cmd_id, None)

    def _resolve(self, msg: dict) -> bool:
        """Try to resolve a pending command future. Returns True if matched."""
        payload = msg.get("payload", {})
        cmd_id = payload.get("id")
        if not cmd_id or cmd_id not in self._pending:
            return False

        fut = self._pending.get(cmd_id)
        if fut and not fut.done():
            if payload.get("ok"):
                fut.set_result(payload.get("result"))
            else:
                fut.set_exception(RuntimeError(payload.get("error", "Unknown bridge error")))
        return True


# Module-level connection instance
bridge = BridgeConnection()


async def handle_ws(websocket: WebSocket) -> None:
    """WebSocket endpoint handler for the WRAITH bridge.

    Accepts the connection, reads messages in a loop, dispatches
    command_result messages to pending futures, and cleans up on disconnect.
    """
    await bridge.connect(websocket)
    try:
        while True:
            try:
                data = await websocket.receive_text()
                msg = json.loads(data)
            except (json.JSONDecodeError, ValueError) as exc:
                log.warning("Invalid JSON from bridge: %s", exc)
                continue

            msg_type = msg.get("type", "")
            log.debug("Bridge message type=%s", msg_type)

            # command_result — resolve a pending future
            if msg_type == "command_result":
                bridge._resolve(msg)
                continue

            # hello — log the extension info
            if msg_type == "hello":
                payload = msg.get("payload", {})
                log.info("Bridge hello: extensionId=%s", payload.get("extensionId"))
                continue

            # status — log and ignore
            if msg_type == "status":
                continue

            log.debug("Unhandled bridge message: %s", msg_type)

    except WebSocketDisconnect:
        log.info("Bridge client disconnected")
    finally:
        await bridge.disconnect()


# ------------------------------------------------------------------
# Router
# ------------------------------------------------------------------

ws_router = APIRouter(tags=["wraith"])


@ws_router.websocket("/wraith/ws")
async def ws_route(websocket: WebSocket) -> None:
    """WRAITH bridge WebSocket endpoint."""
    await handle_ws(websocket)
