"""WRAITH ingestion notifications.

Sends brief iMessage summaries to the General after each ingestion run.
Uses AppleScript for delivery — same approach as services.imessage.daemon.
"""

from __future__ import annotations

import logging
import subprocess

log = logging.getLogger(__name__)

GENERAL_PHONE = "+18704160146"


def _send_imessage(text: str, to: str = GENERAL_PHONE) -> bool:
    """Send an iMessage via AppleScript. Returns True on success."""
    escaped = text.replace("\\", "\\\\").replace('"', '\\"')
    script = f'''
    tell application "Messages"
        set targetService to 1st account whose service type = iMessage
        set targetBuddy to participant "{to}" of targetService
        send "{escaped}" to targetBuddy
    end tell
    '''
    try:
        subprocess.run(
            ["/usr/bin/osascript", "-e", script],
            check=True, timeout=10, capture_output=True,
        )
        log.info("iMessage sent: %s", text[:80])
        return True
    except subprocess.TimeoutExpired:
        log.warning("iMessage send timed out")
        return False
    except Exception as exc:
        log.error("iMessage send failed: %s", exc)
        return False


def notify_ingestion(
    source: str,
    count: int,
    mode: str,
    warnings: list[str] | None = None,
) -> bool:
    """Send an iMessage notification summarizing a WRAITH ingestion run.

    Examples:
        WRAITH: 23 X bookmarks ingested (ghost mode)
        WRAITH: 0 items — Reddit cookies may be expired
    """
    if count > 0:
        msg = f"WRAITH: {count} {source} ingested ({mode} mode)"
    else:
        msg = f"WRAITH: 0 {source} items"

    if warnings:
        msg += " — " + "; ".join(warnings)

    return _send_imessage(msg)
