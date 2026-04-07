# Security Policy

## Scope

WRAITH handles browser-captured content and stores it locally. The security surface includes:

- **WebSocket bridge** (`/wraith/ws`): Accepts connections only on `127.0.0.1`. Not exposed to the network.
- **Safari cookie access**: Read-only access to Safari's binary cookie store for authenticated source fetches (X, Reddit, Audible). Cookies are used in-memory and not persisted separately.
- **API tokens**: `GITHUB_TOKEN` is read from environment variables for GitHub API access. It is never written to disk by WRAITH. YouTube ingestion uses `yt-dlp` (no API key required).
- **Vault files**: Plain markdown with YAML frontmatter. No encryption at rest (by design — the vault is user-readable).

## Reporting Vulnerabilities

If you discover a security vulnerability, please report it privately:

1. Do **not** open a public issue.
2. Email: security@getmodus.dev (or open a private advisory at [github.com/GetModus/wraith](https://github.com/GetModus/wraith/security/advisories)).
3. Include: description, reproduction steps, impact assessment.
4. We will respond within 72 hours.

## Known Security Considerations

- **Cookie jar access**: WRAITH reads Safari's binary cookie store (`~/Library/Containers/com.apple.Safari/Data/Library/Cookies/Cookies.binarycookies`). This requires macOS permissions. The cookies are used for authenticated HTTP requests to source APIs and are not logged, cached, or transmitted.
- **Local-only WebSocket**: The WebSocket server binds to `127.0.0.1`. Any process on the local machine can connect. This is by design — the Safari extension needs localhost access. If you run untrusted code on your machine, it could send captures to WRAITH.
- **No input sanitization on vault writes**: Captured content is written to markdown files as-is. If a malicious page contains content that could be misinterpreted by downstream tools (e.g., YAML injection in frontmatter), it would be written. The Librarian does sanitize frontmatter keys but does not filter body content.
- **Queue file permissions**: `wraith-queue.json`, `wraith-state.json`, and `wraith-officer-handoffs.jsonl` are written with mode `0644`. They contain URLs and titles of captured content. Ensure your data directory has appropriate filesystem permissions.

## Supported Versions

Only the latest release is supported with security updates.
