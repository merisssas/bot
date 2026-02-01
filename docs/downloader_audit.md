# Downloader Audit & Harmonization Notes

Date: 2026-02-01

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

