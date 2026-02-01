# Aria2 Downloader Task (Ultimate Edition)

This module powers the `/aria2dl` Telegram command. It treats Aria2 as a high-performance engine and wraps it with reliability, validation, and operator-friendly UX.

## What This Task Can Do

- Multi-connection segmented downloads (with server support).
- Resume partial downloads (`Range`/`continue`).
- Exponential retry with jitter (transfer stage + Aria2 retries).
- Optional checksum verification and integrity scanning.
- Bandwidth limiting with burst mode.
- Auto rename / overwrite / skip policies.
- Proxy + custom headers + custom user-agent.
- Dry-run mode (metadata probe without downloading).
- Robust logging and consistent error surfacing.

## How It Works

1. **Validate URIs** → sanitize and reject invalid inputs.
2. **Build Aria2 options** → apply config-derived policy (resume, split, retry, checksum, overwrite).
3. **Add to Aria2** → queue and optionally adjust priority.
4. **Track progress** → Telegram message UI updates.
5. **Transfer to storage** → retries, exponential backoff + jitter.

## Configuration (TOML)

All settings are under `[aria2]` in `config.toml`:

```toml
[aria2]
enable = true
url = "http://localhost:6800/jsonrpc"
secret = ""
remove_after_transfer = true

# Reliability
max_retries = 5
retry_base_delay = "500ms"
retry_max_delay = "10s"

# Performance
enable_resume = true
split = 8
max_conn_per_server = 4
min_split_size = "1MB"

# Bandwidth control
limit_rate = ""
burst_rate = "2MB"
burst_duration = "10s"

# Conflict policy: overwrite | rename | skip
overwrite_policy = "rename"

# Integrity
checksum_algorithm = ""
expected_checksum = ""

# UX / networking
user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0"
proxy = ""
headers = { "Authorization" = "Bearer token" }

# Queue priority
default_priority = 0

# Dry-run (metadata only)
dry_run = false
```

## Dry Run

Dry-run mode performs a `HEAD` probe (fallback to `GET` range) for HTTP/HTTPS URLs and reports metadata (filename, size, content type). Non-HTTP URLs are skipped with a warning in the output.

## Notes & Limits

- Multi-connection, resume, and checksum checks require Aria2 support and server capabilities.
- Captcha/DRM-protected URLs cannot be downloaded.
- For magnet/torrent metadata, follow-up GIDs are handled automatically.

## Useful Commands

```text
/aria2dl https://example.com/file.zip
/aria2dl magnet:?xt=urn:btih:...
```
