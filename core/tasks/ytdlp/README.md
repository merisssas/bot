# yt-dlp Task (Ultimate Downloader)

Task ini menjalankan yt-dlp dengan fokus pada **reliabilitas**, **performa**, dan **UX**. Dirancang untuk workflow Telegram bot, tetapi tetap kompatibel untuk kebutuhan otomasi internal lain.

## Fitur Utama

- **Resume download** (HTTP range) via `--continue`.
- **Multi-connection/segmented download** untuk HLS/DASH (concurrent fragments) dan opsional external downloader (`aria2c`).
- **Retry** dengan exponential backoff + jitter.
- **State persistence** (progress + workspace) untuk resume setelah restart.
- **Verifikasi integritas** (checksum opsional) + file checksum sidecar.
- **Bandwidth limit + burst mode** (limit & throttled rate).
- **Adaptive rate limit** per-host + throttling berbasis feedback.
- **Queue + concurrency per task** (parallel per URL).
- **Overwrite policy**: overwrite / auto-rename / skip.
- **Proxy pool** dengan health scoring + auto-rotate.
- **Logging** konsisten (console + file) + level.
- **Dry-run** untuk validasi metadata tanpa download.
- **Progress bar** per-file + total.
- **Format fallback** jika format utama gagal.
- **Fingerprint randomization** (User-Agent + headers).
- **Deduplication** konten sebelum upload.

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
| `--sa-format-sort` | `--sa-format-sort=res:1080,vcodec:h264,acodec:aac` | Override urutan format selection. |
| `--sa-recode` | `--sa-recode=mp4` | Recode video output (opsional). |
| `--sa-merge-format` | `--sa-merge-format=mp4` | Override format merge output. |

Contoh:

```
/ytdlp --sa-dry-run --sa-overwrite=skip --sa-conn=2 https://example.com/video
```

## Konfigurasi (config.toml)

```
[ytdlp]
max_retries = 5
retry_base_delay = "2s"
retry_max_delay = "30s"
retry_jitter = 0.25
download_concurrency = 2
fragment_concurrency = 16
enable_resume = true
proxy = ""
proxy_pool = []
external_downloader = ""
external_downloader_args = []
limit_rate = ""
throttled_rate = ""
adaptive_limit = true
adaptive_limit_min_rate = "512K"
adaptive_limit_max_rate = "0"
overwrite_policy = "rename" # overwrite | rename | skip
format_sort = "res:1080,vcodec:h264,acodec:aac"
format_fallbacks = ["bestvideo+bestaudio/best", "best"]
recode_video = "mp4"
merge_output_format = "mp4"
enable_fragment_repair = true
fragment_repair_passes = 2
dedup_enabled = true
persist_state = true
state_dir = "data/ytdlp_state"
cleanup_state_on_success = true
rate_limit_min_interval = "0ms"
rate_limit_max_interval = "5s"
rate_limit_jitter = 0.2
fingerprint_randomize = true
user_agent_pool = [
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
  "Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/122.0",
]
happy_eyeballs = true
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
