# TFile Audit & Decisions

## Audit (core/tasks/tfile)
**Files scanned**:
- execute.go
- progress.go
- stream.go
- taskinfo.go
- tftask.go
- util.go
- writer.go
- policy.go
- retry.go
- README.md

## Structure Classification
**Core downloader logic**:
- execute.go (download lifecycle, cache handling, upload)
- stream.go (streamed download path)
- tftask.go (task construction)
- taskinfo.go (task metadata interface)

**Helpers**:
- progress.go (Telegram progress messages)
- writer.go (progress-aware writers)
- util.go (progress update policy)
- policy.go (overwrite policy resolution)
- retry.go (backoff retries)

**Legacy / Unused**:
- None detected in this scope.

## Duplication / Conflicts
- No duplicate file names or overlapping functions within `core/tasks/tfile`.
- Downloader logic is unique to this task and does not conflict with `directlinks` (HTTP) or `ytdlp` pipelines.

## Decisions on Unused/Legacy Code
- **KEEP**: All helper utilities are in active use by the Telegram download path.
- **DEPRECATE**: None.
- **REMOVE**: None.

## Harmonization Notes
- Added consistent retry/backoff handling for download + upload.
- Standardized overwrite policy handling.
- Added cache integrity checks for fail-safe behavior.
