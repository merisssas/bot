# Directlinks Ultimate Downloader

This folder contains the **Directlinks** task implementation — the HTTP/HTTPS downloader used by Teleload for direct links. It is designed to deliver a “premium downloader” experience: resilient transfers, segmented downloads, resuming, strict validation, and UX-friendly progress reporting.

## Capabilities

- **Resume downloads** with HTTP Range when supported.
- **Segmented/multi-connection downloads** for large files.
- **Retry with exponential backoff + jitter** for resilient transfers.
- **Checksum verification** (optional).
- **Bandwidth limiting** with burst allowance.
- **Queue with priority** per file (higher priority first).
- **Pause/resume** globally or per task (API hooks).
- **Filename detection + conflict handling** (`overwrite`, `rename`, `skip`).
- **Proxy and basic auth** support.
- **Progress updates**: total + per-file stats.
- **Dry-run** metadata check (no download).

## How It Works

Directlinks is invoked from Telegram handlers and fed a list of URLs. The task:

1. Validates inputs.
2. Performs **HEAD** (or range GET fallback) to detect size, filename, and resumability.
3. Resolves filename conflicts using storage checks.
4. Downloads files with either:
   - **Single stream** (for small or non-resumable resources), or
   - **Multipart range downloads** (for large, resumable resources).
5. Optionally verifies checksum before saving to storage.

## Configuration

Configuration is available via **config file**, **CLI flags**, and **environment variables**.

### Config file (`config.toml`)

```toml
[directlinks]
max_concurrency = 4
segment_concurrency = 16
min_multipart_size = "5MB"
min_segment_size = "1MB"
enable_resume = true
max_retries = 5
retry_base_delay = "500ms"
retry_max_delay = "10s"
limit_rate = "10M"
burst_rate = "2MB"
overwrite_policy = "rename"
dry_run = false
checksum_algorithm = "sha256"
expected_checksum = ""
write_checksum_file = false
log_file = ""
log_level = "info"
user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0"
proxy = ""
auth_username = ""
auth_password = ""
default_priority = 0
```

### CLI flags

```bash
Teleload \
  --directlinks-max-concurrency=4 \
  --directlinks-segment-concurrency=16 \
  --directlinks-min-multipart-size=5MB \
  --directlinks-min-segment-size=1MB \
  --directlinks-enable-resume=true \
  --directlinks-max-retries=5 \
  --directlinks-retry-base-delay=500ms \
  --directlinks-retry-max-delay=10s \
  --directlinks-limit-rate=10M \
  --directlinks-burst-rate=2MB \
  --directlinks-overwrite-policy=rename \
  --directlinks-dry-run=false \
  --directlinks-checksum-algorithm=sha256 \
  --directlinks-expected-checksum= \
  --directlinks-write-checksum-file=false \
  --directlinks-log-file= \
  --directlinks-log-level=info \
  --directlinks-user-agent="Mozilla/5.0 ... UltimateDownloader/2.0" \
  --directlinks-proxy= \
  --directlinks-auth-username= \
  --directlinks-auth-password= \
  --directlinks-default-priority=0
```

### Environment variables

All config keys map to `TELELOAD_*` environment variables. Example:

```bash
export TELELOAD_DIRECTLINKS_MAX_CONCURRENCY=4
export TELELOAD_DIRECTLINKS_LIMIT_RATE=10M
export TELELOAD_DIRECTLINKS_DRY_RUN=true
```

## Operational Notes

- **Resume** only works when servers support HTTP Range.
- **Multipart downloads** are used only when size is known and range is supported.
- **Checksum validation** requires a supported algorithm: `sha256`, `sha1`, or `md5`.
- **Conflict policy**:
  - `overwrite`: replace existing files.
  - `rename`: auto-append `(1)`, `(2)`, ...
  - `skip`: fail fast if the target exists.
- **Pause/Resume**:
  - Global: `directlinks.PauseAll()` / `directlinks.ResumeAll()`
  - Per task: `task.Pause()` / `task.Resume()`

## Troubleshooting

- **Stuck at 0%**: server may block HEAD; enable range fallback is automatic.
- **Cannot resume**: server doesn’t support `Accept-Ranges: bytes`.
- **Checksum mismatch**: re-download or confirm expected checksum value.
- **Slow speeds**: check `limit_rate` or proxy configuration.

## Limitations

- Captcha/DRM-protected URLs are not supported.
- Some servers throttle or block aggressive segmented downloads.
- Proxy authentication must be provided via proxy URL or directlinks auth fields.
