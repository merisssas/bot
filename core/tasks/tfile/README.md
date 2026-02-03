# TFile Task (Telegram File Downloader)

## Overview
The `tfile` task is responsible for downloading Telegram files and uploading them to a configured storage backend. It emphasizes reliability, safe retries, and clear operational logging while staying within Telegram API constraints.

## Key Capabilities
- **Parallel downloads** via gotd downloader threads (multi-connection semantics for Telegram file parts).
- **Retry with exponential backoff + jitter** for download and upload stages.
- **Dry-run** mode to validate metadata without transferring bytes.
- **Overwrite policies** (`rename`, `overwrite`, `skip`) for destination conflicts.
- **Cache validation**: reuses valid cached files and rejects stale/partial caches.
- **Optional hash verification** when Telegram provides file hashes.

## Configuration
Add the `[tfile]` section to `config.toml`:

```toml
[tfile]
max_retries = 3
retry_base_delay = "500ms"
retry_max_delay = "10s"
retry_jitter = 0.25
overwrite_policy = "rename" # rename | overwrite | skip
dry_run = false
verify_hashes = true
```

## Execution Flow (High-Level)
1. Validate input metadata.
2. Resolve destination path using the overwrite policy.
3. Start progress tracking.
4. Dry-run (optional).
5. Download with retries (cache-backed).
6. Verify size, detect extension if needed.
7. Upload to storage with retries.
8. Clean cache (unless `no_clean_cache` is set).

## Operational Notes
- **Stream mode** (`stream=true`) is intended for low-disk environments but does **not** support retrying partial uploads (storage APIs do not expose delete/rollback).
- **Resume support** is limited to reusing complete cache files; Telegram’s MTProto downloader does not expose HTTP range semantics.

## Troubleshooting
- **Size mismatch errors**: cache or download is partial/corrupt. Clear the cache directory and retry.
- **Overwrite conflicts**: use `overwrite_policy = "rename"` or `"overwrite"` as needed.
- **Slow downloads**: increase global `threads` (Telegram side) or reduce contention via lower workers.

## Known Limitations
- Telegram downloads are MTProto-based; HTTP range semantics and third-party proxy credentials are not applicable.
- Bandwidth limiting for MTProto downloads is not currently supported.
