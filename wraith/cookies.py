"""Safari binary cookie extraction.

Parses ~/Library/Cookies/Cookies.binarycookies and provides cookies
in formats suitable for httpx, Playwright, or raw Cookie headers.
"""

from __future__ import annotations

import logging
import struct
from datetime import datetime, timezone, timedelta
from pathlib import Path

log = logging.getLogger(__name__)

# Mac epoch: 2001-01-01 00:00:00 UTC
_MAC_EPOCH = datetime(2001, 1, 1, tzinfo=timezone.utc)

_DEFAULT_PATH = Path.home() / "Library" / "Containers" / "com.apple.Safari" / "Data" / "Library" / "Cookies" / "Cookies.binarycookies"

# Cookie flag bitmask
_FLAG_SECURE = 0x1
_FLAG_HTTPONLY = 0x4


def _mac_epoch_to_datetime(timestamp: float) -> datetime | None:
    """Convert a Mac absolute time (seconds since 2001-01-01) to a UTC datetime."""
    if timestamp == 0:
        return None
    try:
        return _MAC_EPOCH + timedelta(seconds=timestamp)
    except (OverflowError, ValueError):
        return None


def _read_cstring(data: bytes, offset: int) -> str:
    """Read a null-terminated string from *data* starting at *offset*."""
    end = data.index(b"\x00", offset)
    return data[offset:end].decode("ascii", errors="replace")


def _parse_cookie_record(data: bytes) -> dict | None:
    """Parse a single cookie record from page-relative bytes."""
    if len(data) < 44:
        return None

    (size,) = struct.unpack_from("<I", data, 0)
    (flags,) = struct.unpack_from("<I", data, 4)
    # offsets 8-15: padding (8 bytes)
    (url_offset,) = struct.unpack_from("<I", data, 16)
    (name_offset,) = struct.unpack_from("<I", data, 20)
    (path_offset,) = struct.unpack_from("<I", data, 24)
    (value_offset,) = struct.unpack_from("<I", data, 28)
    # offsets 32-39: comment (8 bytes, skip)
    (expiry,) = struct.unpack_from("<d", data, 40)
    # creation at offset 44 — only present if record is large enough
    # (creation_date,) = struct.unpack_from("<d", data, 44) if len(data) >= 52 else (0.0,)

    try:
        domain = _read_cstring(data, url_offset)
        name = _read_cstring(data, name_offset)
        path = _read_cstring(data, path_offset)
        value = _read_cstring(data, value_offset)
    except (ValueError, IndexError):
        return None

    expires_dt = _mac_epoch_to_datetime(expiry)

    return {
        "name": name,
        "value": value,
        "domain": domain,
        "path": path,
        "expires": expires_dt.isoformat() if expires_dt else None,
        "secure": bool(flags & _FLAG_SECURE),
        "httponly": bool(flags & _FLAG_HTTPONLY),
    }


def _parse_page(page_data: bytes) -> list[dict]:
    """Parse a single page and return its cookie records."""
    if len(page_data) < 8:
        return []

    (header,) = struct.unpack_from(">I", page_data, 0)
    if header != 0x00000100:
        log.warning("Unexpected page header: 0x%08X", header)
        return []

    (num_cookies,) = struct.unpack_from("<I", page_data, 4)
    cookies: list[dict] = []

    for i in range(num_cookies):
        offset_pos = 8 + i * 4
        if offset_pos + 4 > len(page_data):
            break
        (cookie_offset,) = struct.unpack_from("<I", page_data, offset_pos)
        record_data = page_data[cookie_offset:]
        cookie = _parse_cookie_record(record_data)
        if cookie is not None:
            cookies.append(cookie)

    return cookies


def parse_binary_cookies(path: Path | str | None = None) -> list[dict]:
    """Parse Safari's Cookies.binarycookies file.

    Returns a list of cookie dicts with keys:
        name, value, domain, path, expires, secure, httponly
    """
    filepath = Path(path) if path else _DEFAULT_PATH

    if not filepath.exists():
        log.warning("Cookie file not found: %s", filepath)
        return []

    try:
        data = filepath.read_bytes()
    except (OSError, PermissionError) as exc:
        log.warning("Cannot read cookie file %s: %s", filepath, exc)
        return []

    if len(data) < 8 or data[:4] != b"cook":
        log.warning("Invalid binarycookies magic in %s", filepath)
        return []

    (num_pages,) = struct.unpack_from(">I", data, 4)

    # Read page sizes (big-endian uint32 array)
    page_sizes: list[int] = []
    for i in range(num_pages):
        (ps,) = struct.unpack_from(">I", data, 8 + i * 4)
        page_sizes.append(ps)

    # Pages start after header: 4 (magic) + 4 (num_pages) + num_pages * 4 (sizes)
    page_offset = 8 + num_pages * 4
    cookies: list[dict] = []

    for size in page_sizes:
        if page_offset + size > len(data):
            log.warning("Page extends beyond file, truncating")
            break
        page_data = data[page_offset : page_offset + size]
        cookies.extend(_parse_page(page_data))
        page_offset += size

    log.debug("Parsed %d cookies from %s", len(cookies), filepath)
    return cookies


def extract_cookies(
    domains: list[str],
    path: Path | str | None = None,
) -> dict[str, str]:
    """Extract cookies for the given domains as a simple {name: value} dict.

    Domain matching is suffix-based: a cookie with domain ``.example.com``
    matches a request for ``example.com`` or ``www.example.com``.
    """
    all_cookies = parse_binary_cookies(path)
    result: dict[str, str] = {}

    for cookie in all_cookies:
        cookie_domain = cookie["domain"].lstrip(".")
        for domain in domains:
            target = domain.lstrip(".")
            if cookie_domain == target or cookie_domain.endswith("." + target) or target.endswith("." + cookie_domain):
                result[cookie["name"]] = cookie["value"]
                break

    return result


def cookies_to_header(cookies: dict[str, str]) -> str:
    """Format a cookie dict as a ``Cookie`` header value.

    Example: ``"session=abc123; token=xyz"``
    """
    return "; ".join(f"{name}={value}" for name, value in cookies.items())


def cookies_to_playwright(cookies: dict[str, str], domain: str) -> list[dict]:
    """Format cookies for Playwright's ``context.add_cookies()``.

    Each entry gets the provided *domain* and sensible defaults for
    path and sameSite.
    """
    normalized = domain if domain.startswith(".") else f".{domain}"
    return [
        {
            "name": name,
            "value": value,
            "domain": normalized,
            "path": "/",
            "sameSite": "Lax",
        }
        for name, value in cookies.items()
    ]
