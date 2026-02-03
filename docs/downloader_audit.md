# Downloader Audit & Harmonization Notes

Date: 2026-02-01 (updated 2026-02-02)

## 1) Audit Scope (Repo Scan)

Full repo scan performed via `rg --files` to enumerate every tracked file and identify downloader-related surfaces, helpers, and legacy/stub code paths.

## 2) Core vs Helper vs Legacy (Downloader-Focused)

### Core download/task flow
- `core/core.go` (task execution pipeline, queue hooks)
- `core/tasks/aria2dl/*` (aria2 download task execution + progress)
- `core/tasks/tfile/*`, `core/tasks/transfer/*`, `core/tasks/directlinks/*`, `core/tasks/parsed/*`, `core/tasks/ytdlp/*` (other download/transfer tasks)
- `pkg/aria2/client.go` (aria2 RPC client)
- `pkg/queue/*` (task queue)

### Helpers and shared utilities
- `common/utils/dlutil/*` (download speed helpers)
- `common/utils/netutil/proxy.go` (proxy helper)
- `common/utils/fsutil/*`, `common/utils/strutil/*` (filesystem/string helpers)
- `common/utils/ioutil/*` (progress reader + writer helpers)
- `storage/*` and `config/storage/*` (storage backends & configuration)
- `config/*` (config loading + defaults)

### Legacy/stub/optional paths
- `cmd/upload/progress_stub.go` (CLI stub for progress)
- `parsers/js/api_playwright_stub.go`, `parsers/js/js_stub.go` (Playwright/JS stubs)
- `storage/minio/client_stub.go` (MinIO stub)
- `storage/rclone/*` (optional backend)

## 3) Duplicate/Overlapping Components

- Progress renderers:
  - `cmd/upload/progress_tea.go` vs `cmd/upload/progress_stub.go`
  - **Decision**: KEEP both (build tags/optional TUI), document usage.
- Playwright/JS runtime:
  - `parsers/js/api_playwright.go` vs `parsers/js/api_playwright_stub.go`
  - `parsers/js/js.go` vs `parsers/js/js_stub.go`
  - **Decision**: KEEP both (stub for environments without Playwright).
- MinIO client:
  - `storage/minio/client.go` vs `storage/minio/client_stub.go`
  - **Decision**: KEEP both (stub for builds without MinIO).

## 4) Harmonization Notes

- Use `log.FromContext(ctx)` consistently for task logging.
- Normalize progress messaging via i18n keys (no raw strings in user-visible text).
- Prefer `Task` accessors for state (`GID()`, `Snapshot()`, `UpdateStats()`) to avoid races.

## 5) Unused/Unclear Functions & Decisions

- No hard removals were made in this pass. When a helper is found unused:
  - **KEEP** if it is required for optional backends or future feature parity.
  - **DEPRECATE** by adding doc comments and a minimal test.
  - **REMOVE** only when duplication adds real maintenance risk.

## 6) Open Items / Roadmap Hooks

- Add segmented HTTP range downloads for non-aria2 flows.
- Extend checksum verification into storage pipelines.
- Implement dry-run metadata probes for CLI flows.
- Document CLI flags + env + config overrides for downloader behaviors.

## 7) Directlinks Audit Update (2026-02-02)

### Folder scan (directlinks scope)
- `core/tasks/directlinks/task.go`
- `core/tasks/directlinks/execute.go`
- `core/tasks/directlinks/util.go`
- `core/tasks/directlinks/progress.go`
- `core/tasks/directlinks/README.md`

### Duplicate/conflict checks
- No duplicate filenames within directlinks scope.
- No conflicting function names or overlapping logic detected beyond shared helpers.
- No dependency conflicts introduced in directlinks scope.

### Core vs helper vs legacy (directlinks scope)
- **Core**: `execute.go`, `task.go`
- **Helper**: `util.go`, `progress.go`
- **Docs**: `README.md`
- **Legacy**: none identified

### Unused function/module decisions
- All directlinks helpers are actively used or part of the new “Ultimate Downloader” feature set.
- **Decision**: KEEP all functions; no DEPRECATE/REMOVE actions required in this pass.

## 8) Aria2DL Audit Update (2026-02-03)

### Folder scan (aria2dl scope)
- `core/tasks/aria2dl/task.go`
- `core/tasks/aria2dl/execute.go`
- `core/tasks/aria2dl/progress.go`
- `core/tasks/aria2dl/options.go`
- `core/tasks/aria2dl/dryrun.go`
- `core/tasks/aria2dl/format.go`
- `core/tasks/aria2dl/validate.go`
- `core/tasks/aria2dl/README.md`

### Duplicate/conflict checks
- Consolidated duplicate byte-formatting logic into `FormatBytes` for shared reuse.
- No conflicting function names across downloader tasks after validation helpers were added.

### Core vs helper vs legacy (aria2dl scope)
- **Core**: `execute.go`, `task.go`
- **Helper**: `options.go`, `dryrun.go`, `format.go`, `validate.go`
- **Docs**: `README.md`
- **Legacy**: none identified

### Unused function/module decisions
- **KEEP**: validation + dry-run helpers (directly used by aria2 download entrypoint).
- **KEEP**: priority helper (`ApplyQueuePriority`) for queue ordering.

## 9) YT-DLP Resilience Roadmap (2026-02-04)

This section captures hardening recommendations for yt-dlp-backed flows so they can remain competitive against IDM/JDownloader-style reliability while still fitting Teleload’s architecture.

### High-impact reliability goals
- **Retry engine with exponential backoff + jitter** for transient HTTP errors and rate limits.
- **Persistent progress state** (per format + fragment) to survive bot restarts and crashes.
- **Adaptive rate limiting** per host/IP/ASN to reduce bans and throttling.
- **Proxy pool with health scoring** (latency, success rate, bandwidth) for aggressive block environments.
- **Fragment retry/repair** for DASH/HLS segments to prevent corrupt merges.

### Priority implementation order
| #  | Feature | Impact | Difficulty | Notes |
|---:|---------|:------:|:----------:|-------|
| 1 | Retry with exponential backoff + jitter | 10 | Medium | Required for Cloudflare/Akamai resiliency |
| 2 | Persistent state for format + fragment | 10 | High | Enables resume after restart |
| 3 | HTTP/3 + dual-stack + happy eyeballs | 9 | Medium | Significant stability gains on poor ISPs |
| 4 | Proxy rotator + scoring | 9 | High | IP-blocked sites become feasible |
| 5 | Fragment-level retry + repair | 8 | High | Stabilizes DASH/HLS downloads |
| 6 | Adaptive bandwidth throttling | 8 | Med-High | Avoids packet loss/disconnects |
| 7 | Multi-connection per file | 7–9 | Very High | Requires custom downloader |
| 8 | Deduplication (content + name) | 7 | Medium | Use xxh3/128 or blake3 |
| 9 | Smart format sorting | 7 | Medium | Better defaults for users |
|10 | Anti-ban fingerprint randomization | 6–8 | High | UA/TLS/HTTP2/QUIC tuning |
|11 | Parallel extraction + queue prioritization | 6 | Medium | Helps large playlists |
|12 | Auto-retry with format fallback | 6 | Medium | 1080p → 720p fallback |

### Guiding strategy
- Treat yt-dlp primarily as **extractor + format selector**.
- For IDM-level behavior, invest in a **custom segmented downloader** with range-based resume/repair.
- When staying within yt-dlp, focus on **state persistence + proxy health + intelligent retry** first.
