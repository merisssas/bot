# Audit & Harmonization Notes (core/tasks/ytdlp)

## 1) Audit Scope

Folder audited:

- `core/tasks/ytdlp/execute.go`
- `core/tasks/ytdlp/progress.go`
- `core/tasks/ytdlp/task.go`

## 2) Core vs Helper vs Legacy

**Core / Downloader Flow**

- `execute.go` (download pipeline, retry, transfer)
- `task.go` (task model + validation)

**Helper / UI**

- `progress.go` (progress aggregation + Telegram UI updates)

**Legacy / Unused**

- Tidak ditemukan file legacy di folder ini.

## 3) Duplicate / Overlap Check

- Tidak ada duplikasi fungsi antar file yang berdampak langsung.
- Overlap minor (string formatting/logging) sudah diseragamkan di task logger.

## 4) Harmonization Decisions

- **KEEP** semua file inti.
- **DEPRECATE**: tidak ada.
- **REMOVE**: tidak ada.

## 5) Changelog / Notes

- Menambahkan konfigurasi terstruktur, control flags, dan task logger.
- Menambahkan queue per-task (parallel per URL) + retry dengan jitter.
- Menambahkan checksum optional + overwrite policy.
- Menambahkan dry-run dan progress total + per-file.
