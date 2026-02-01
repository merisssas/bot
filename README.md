<div align="center">

# <img src="docs/static/logo.png" width="45" align="center"> Teleload

**English** | [简体中文](./README_zh.md)

> **Save Telegram files to any storage, bypass restricted content, and automate downloads from the web.**

[![Release Date](https://img.shields.io/github/release-date/krau/teleload?label=release)](https://github.com/krau/teleload/releases)
[![tag](https://img.shields.io/github/v/tag/krau/teleload.svg)](https://github.com/krau/teleload/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/krau/teleload/build-release.yml)](https://github.com/krau/teleload/actions/workflows/build-release.yml)
[![Stars](https://img.shields.io/github/stars/krau/teleload?style=flat)](https://github.com/krau/teleload/stargazers)
[![Downloads](https://img.shields.io/github/downloads/krau/teleload/total)](https://github.com/krau/teleload/releases)
[![Issues](https://img.shields.io/github/issues/krau/teleload)](https://github.com/krau/teleload/issues)
[![Pull Requests](https://img.shields.io/github/issues-pr/krau/teleload?label=pr)](https://github.com/krau/teleload/pulls)
[![License](https://img.shields.io/github/license/krau/teleload)](./LICENSE)

</div>

## ✨ Overview

Teleload adalah bot Telegram yang bisa menyimpan file/pesan dari Telegram dan berbagai website ke banyak backend storage (local, S3, MinIO, WebDAV, AList, dan Telegram). Teleload juga mendukung parser plugin (JavaScript) untuk menangani website yang belum didukung dan pipeline otomasi berbasis aturan.

## 🎯 Fitur Utama

### Penyimpanan & Transfer
- Simpan dokumen / video / foto / stiker / audio / voice / media grup Telegram.
- Bypass **restrict saving content** untuk media tertentu.
- Auto organize berdasarkan aturan (rules) dan folder.
- Transfer antar storage (misalnya Telegram → S3 / Local → WebDAV).
- Streaming transfer agar file besar tetap stabil.
- Batch download dengan queue.

### Otomasi & Monitor
- Watch chat/kanal/grup tertentu dan auto-save pesan.
- Filter berdasarkan keyword, tipe file, ukuran, atau aturan kustom.
- Hooks lifecycle task: before start, success, fail, cancel.

### Ekstensi & Integrasi
- Dukungan plugin JS (Goja) + Playwright untuk parsing website.
- Integrasi `yt-dlp` untuk 1000+ website (video & audio).
- Integrasi `aria2` untuk URL/magnet download.
- Multi-user dengan pembatasan akses.

### Storage Backends
- Local filesystem
- S3 / MinIO
- WebDAV
- AList
- Telegram (re-upload)
- Rclone (via command line)

## 🧭 Arsitektur Singkat

- **CLI Entry**: `main.go` → `cmd.Execute(ctx)`
- **Config**: Viper + file `config.toml` + env `TELELOAD_*`
- **Storage**: plugin factory (`storage/`)
- **Tasks**: `core/tasks/*` melalui queue `pkg/queue`
- **Parsers**: JS plugins di `plugins/`

## ✅ Prasyarat

- Go 1.24.2 (jika build dari source)
- Docker (opsional, jika jalankan via container)
- Telegram bot token dari @BotFather
- (Opsional) Telegram API ID/Hash untuk userbot
- (Opsional) `yt-dlp`, `aria2c`, `rclone` jika fitur dipakai

## 🚀 Quick Start

### 1) Siapkan `config.toml`

Buat file `config.toml` di root project:

```toml
lang = "en" # "en" atau "id"

[telegram]
token = "YOUR_BOT_TOKEN" # dari @BotFather

[telegram.proxy]
enable = false
url = "socks5://127.0.0.1:7890"

[[storages]]
name = "Local Disk"
type = "local"
enable = true
base_path = "./downloads"

[[users]]
id = 123456789 # Telegram user ID kamu
storages = ["Local Disk"]
blacklist = false
```

### 2) Jalankan via Docker

```bash
docker run -d --name teleload \
  -v ./config.toml:/app/config.toml \
  -v ./downloads:/app/downloads \
  ghcr.io/krau/teleload:latest
```

### 3) Jalankan via Binary (Build dari Source)

```bash
go build -o teleload .
./teleload
```

## 🛠️ Panduan Setup Lengkap

### A. Setup Token Bot Telegram
1. Buka @BotFather di Telegram.
2. Jalankan `/newbot` dan ikuti instruksi.
3. Salin token dan masukkan ke `config.toml`.

### B. Tambahkan User yang Diizinkan
Tambahkan user ID ke `[[users]]` agar hanya user tertentu yang bisa menggunakan bot.

```toml
[[users]]
id = 123456789
storages = ["Local Disk"]
blacklist = false
```

### C. Konfigurasi Storage
Contoh lengkap beberapa storage:

#### Local
```toml
[[storages]]
name = "Local Disk"
type = "local"
enable = true
base_path = "./downloads"
```

#### S3 / MinIO
```toml
[[storages]]
name = "S3 Backup"
type = "s3"
enable = true
bucket = "teleload-backup"
region = "ap-southeast-1"
endpoint = "https://s3.amazonaws.com"
access_key = "YOUR_ACCESS_KEY"
secret_key = "YOUR_SECRET_KEY"
```

#### WebDAV
```toml
[[storages]]
name = "WebDAV"
type = "webdav"
enable = true
url = "https://example.com/dav"
username = "user"
password = "pass"
base_path = "/teleload"
```

#### Telegram (Re-upload)
```toml
[[storages]]
name = "Telegram Archive"
type = "telegram"
enable = true
chat_id = -1001234567890
```

### D. Setup Storage Rules
```toml
[[rules]]
name = "Videos Only"
match = "video"
storage = "Local Disk"
```

### E. Setup Watch Chat
```toml
[[watch_chats]]
chat_id = -1001234567890
filters = ["video", "document"]
```

## 🧪 Command & Operasional

### CLI Commands
```bash
./teleload --help
./teleload version
```

### Menjalankan di Docker Compose
```bash
docker compose up -d
```

### Build & Test
```bash
# Build
CGO_ENABLED=0 go build -o teleload .

# Run
./teleload

# Test
go test ./...

# Lint
go vet ./...

# Format
go fmt ./...
```

## 🧩 Plugin JS (Parser)

1. Buat file JS baru di `plugins/`.
2. Gunakan API berikut:

```javascript
registerParser({
  metadata: {
    name: "ExampleParser",
    version: "1.0.0",
  },
  canHandle: (url) => url.includes("example.com"),
  parse: async ({ url, request }) => {
    const html = await request(url)
    return {
      title: "Example",
      items: [{
        url: "https://example.com/file.mp4",
        filename: "file.mp4",
      }],
    }
  },
})
```

## 🔧 Environment Variables

Semua config bisa di-set via env dengan prefix `TELELOAD_`:

```bash
export TELELOAD_TELEGRAM_TOKEN="YOUR_BOT_TOKEN"
export TELELOAD_DB_PATH="./data/teleload.db"
```

## ❓ FAQ

**Q: Bagaimana menambah user baru?**
A: Tambahkan `[[users]]` di `config.toml`.

**Q: Bisa simpan ke Google Drive?**
A: Gunakan Rclone backend dan konfigurasi di server.

**Q: File terlalu besar, gagal upload?**
A: Pastikan storage mendukung ukuran file, dan gunakan streaming transfer.

## 🧯 Troubleshooting

- **Bot tidak merespon** → cek token di `config.toml`, cek log container.
- **Storage error** → verifikasi credential dan koneksi network.
- **Parser gagal** → cek JS plugin & Playwright dependency.

## 📚 Dokumentasi Tambahan

- Dokumentasi lengkap: `docs/`
- Example config: `config.example.toml`

## Sponsors

This project is supported by [YxVM](https://yxvm.com/) and [NodeSupport](https://github.com/NodeSeekDev/NodeSupport).

If this project is helpful to you, consider sponsoring me via:

- [Afdian](https://afdian.com/a/unvapp)

## Thanks To

- [gotd](https://github.com/gotd/td)
- [TG-FileStreamBot](https://github.com/EverythingSuckz/TG-FileStreamBot)
- [gotgproto](https://github.com/celestix/gotgproto)
- [tdl](https://github.com/iyear/tdl)
- All the dependencies, contributors, sponsors and users.

## Contact

- [![Group](https://img.shields.io/badge/Teleload-Group-blue)](https://t.me/ProjectTeleload)
- [![Discussion](https://img.shields.io/badge/Github-Discussion-white)](https://github.com/krau/teleload/discussions)
- [![PersonalChannel](https://img.shields.io/badge/Krau-PersonalChannel-cyan)](https://t.me/acherkrau)
