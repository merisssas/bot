# yt-dlp Task (Ultimate Downloader)

Task ini menjalankan yt-dlp dengan fokus pada **reliabilitas**, **performa**, dan **UX**. Dirancang untuk workflow Telegram bot, tetapi tetap kompatibel untuk kebutuhan otomasi internal lain.

## Fitur Utama

- **Resume download** (HTTP range) via `--continue`.
- **Multi-connection/segmented download** untuk HLS/DASH (concurrent fragments) dan opsional external downloader (`aria2c`).
- **Retry** dengan exponential backoff + jitter.
- **Verifikasi integritas** (checksum opsional) + file checksum sidecar.
- **Bandwidth limit + burst mode** (limit & throttled rate).
- **Queue + concurrency per task** (parallel per URL).
- **Overwrite policy**: overwrite / auto-rename / skip.
- **Proxy & auth** via yt-dlp `--proxy`.
- **Logging** konsisten (console + file) + level.
- **Dry-run** untuk validasi metadata tanpa download.
- **Progress bar** per-file + total.

## Cara Pakai (Bot)

Perintah dasar:

```
/ytdlp https://example.com/video
```

Multiple URL:

```
/ytdlp https://example.com/video1 https://example.com/video2
```

Custom yt-dlp flags:

```
/ytdlp --extract-audio --audio-format mp3 https://example.com/video
```

## Control Flags (Teleload)

Control flags diawali dengan `--sa-` (atau `--Teleload-`) dan **tidak** diteruskan ke yt-dlp.

| Flag | Contoh | Deskripsi |
| --- | --- | --- |
| `--sa-dry-run` | `--sa-dry-run` | Validasi metadata tanpa download. |
| `--sa-overwrite` | `--sa-overwrite=rename` | `overwrite` / `rename` / `skip`. |
| `--sa-checksum` | `--sa-checksum=sha256:deadbeef` | Verifikasi checksum. |
| `--sa-checksum-file` | `--sa-checksum-file` | Buat file checksum sidecar. |
| `--sa-proxy` | `--sa-proxy=socks5://user:pass@host:1080` | Proxy untuk yt-dlp. |
| `--sa-limit` | `--sa-limit=8M` | Limit bandwidth download. |
| `--sa-burst` | `--sa-burst=500K` | Throttled rate (burst control). |
| `--sa-conn` | `--sa-conn=3` | Jumlah download URL paralel. |
| `--sa-fragments` | `--sa-fragments=16` | Concurrent fragments HLS/DASH. |
| `--sa-retries` | `--sa-retries=5` | Retry per URL. |
| `--sa-priority` | `--sa-priority=2` | Priority display & queue order hint. |

Contoh:

```
/ytdlp --sa-dry-run --sa-overwrite=skip --sa-conn=2 https://example.com/video
```

## Konfigurasi (config.toml)

```
[ytdlp]
max_retries = 5
download_concurrency = 2
fragment_concurrency = 16
enable_resume = true
proxy = ""
external_downloader = ""
external_downloader_args = []
limit_rate = ""
throttled_rate = ""
overwrite_policy = "rename" # overwrite | rename | skip
dry_run = false
checksum_algorithm = ""
expected_checksum = ""
write_checksum_file = false
log_file = ""
log_level = "info"
user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0"
```

Semua opsi di atas bisa di-override via environment variable `TELELOAD_YTDLP_*`, contoh:

```
TELELOAD_YTDLP_DOWNLOAD_CONCURRENCY=3
TELELOAD_YTDLP_FRAGMENT_CONCURRENCY=32
```

## Troubleshooting

- **Server tidak mendukung range**: `enable_resume` tidak efektif, download berjalan normal.
- **HLS/DASH lambat**: naikkan `fragment_concurrency` (mis. 16 → 32).
- **File duplikat**: gunakan `overwrite_policy=rename` atau `--sa-overwrite=skip`.
- **Proxy error**: pastikan format proxy valid dan kredensial benar.

## Batasan

- DRM, captcha, atau login flow tertentu tetap membutuhkan intervensi manual.
- Beberapa platform membatasi kecepatan untuk user-agent tertentu.

## Error Codes (Internal)

`TaskError` memiliki `ExitCode()` untuk integrasi CLI/automation:

| Code | Exit |
| --- | --- |
| `invalid_input` | 2 |
| `workspace` | 3 |
| `download_failed` | 4 |
| `transfer_failed` | 5 |
| `integrity_failed` | 6 |
| `config_error` | 7 |
| `canceled` | 130 |
