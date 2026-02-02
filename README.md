# Teleload

Teleload is a production-grade Telegram bot that reliably captures files and messages from Telegram and the web, then delivers them to your preferred storage backends. It is designed for scale, observability, and extensibility, with a modular architecture that supports pluggable parsers, multiple storage targets, and robust automation workflows.

## Why Teleload

**Purpose-built for serious workloads.** Teleload is engineered for teams and power users who need dependable automation, consistent storage rules, and a clean operational footprint. It delivers a stable, secure, and extensible foundation that can grow from a single-instance deployment to complex, multi-user environments.

## Key Capabilities

- 🚀 **Multi-source ingestion**: Save files and messages from Telegram and external websites.
- 🗂️ **Multi-backend storage**: Store to local disk, WebDAV, S3-compatible storage, AList, Rclone, or Telegram.
- 🧩 **Parser plugins**: Extend URL handling with JavaScript-based parsers, plus built-in Go parsers.
- 🛠️ **Task orchestration**: Concurrent workers, retries, and progress tracking for reliable transfers.
- 📜 **Rules & automation**: Rule-based routing and automatic organization of files.
- ⚡ **Power tools**: Aria2 and yt-dlp integrations for advanced download scenarios.
- 🛡️ **Operational safety**: Rate limiting, context-aware logging, and structured error handling.

## Architecture Overview

Teleload is composed of clear, scalable modules:

- 🧠 **Core task engine**: Orchestrates download, parsing, and transfer tasks.
- 📦 **Storage abstraction**: Uniform interface across local, WebDAV, S3, Telegram, and more.
- 🧪 **Parser layer**: Built-in parsers plus a plugin system for custom sources.
- 🤖 **Bot client**: Telegram bot handlers, command routing, and middleware.
- 🗃️ **Database layer**: SQLite-backed persistence with GORM.

## Getting Started

### Prerequisites

- ✅ Go **1.24.2**
- ✅ A Telegram Bot Token from [BotFather](https://t.me/botfather)
- ✅ Optional: Telegram API ID & Hash from [my.telegram.org](https://my.telegram.org)

### Quickstart (Binary)

```bash
# Build
go build -o Teleload .

# Run
./Teleload
```

### Quickstart (Docker)

```bash
docker compose up -d
```

## Configuration

Teleload uses a `config.toml` file in the working directory. If it does not exist, the bot generates a default file on startup.

### Minimal Example

```toml
lang = "en"
workers = 3
retry = 3
threads = 4
stream = false

[telegram]
token = "YOUR_BOT_TOKEN"

[[storages]]
name = "Local-1"
type = "local"
base_path = "./downloads"
```

### Key Configuration Sections

- **Global settings**: `lang`, `workers`, `retry`, `threads`, `stream`, `proxy`
- **Telegram**: `token`, `app_id`, `app_hash`, `proxy`, `userbot`
- **Aria2**: `enable`, `url`, `secret`, `remove_after_transfer`
- **Storage definitions**: `[[storages]]`
- **User access control**: `[[users]]`
- **Hooks**: `task_before_start`, `task_success`, `task_fail`, `task_cancel`
- **Parser plugins**: `plugin_enable`, `plugin_dirs`

> For a complete, commented template, see `config.example.toml`.

## Supported Storage Backends

- 🗂️ Local filesystem
- ☁️ WebDAV
- 🪣 S3-compatible (AWS S3, MinIO)
- 📚 AList
- 🔁 Rclone
- 💬 Telegram (re-upload to target chats)

## Parser Plugins

Teleload supports JavaScript-based parser plugins. Each plugin declares metadata and implements `canHandle()` and `parse()` functions. This allows you to add new websites and workflows without modifying the core application.

Example plugins live in the `plugins/` directory.

## Operations & Monitoring

- 📊 **Structured logging**: Context-aware logging with `charmbracelet/log`.
- 📈 **Progress tracking**: Real-time progress updates in Telegram messages.
- ♻️ **Retry strategy**: Configurable retries per task.
- 🧵 **Concurrency control**: Worker pool and download threading limits.

## yt-dlp Ultimate Downloader (HLS-Optimized)

Teleload ships an HLS-tuned yt-dlp task engine designed for maximum throughput and resilience without external downloaders. Highlights:

- **Native concurrent fragments**: Uses yt-dlp's built-in concurrent fragment downloading to saturate bandwidth on HLS streams.
- **Resilient fragment logic**: Infinite fragment retries, resilient file access, and buffer resizing to reduce corrupted segments.
- **Self-healing workspace**: Ensures the temp base path exists and isolates each task in a dedicated workspace.
- **Strict artifact validation**: Filters temporary/partial files and skips suspiciously small outputs.
- **Smart Telegram dashboard**: Live progress bar, spinner, and throttled updates to avoid Telegram flood limits.
- **Hardened task creation**: RFC-compliant URL validation, path traversal protection, and sanitized flags.

These features are purpose-built to compete with IDM/JDownloader-class experiences while staying native to yt-dlp.

## Tutorial: From Zero to Downloading 🚀

### 1) Build and run locally

```bash
go build -o teleload .
./teleload
```

On first run Teleload creates `config.toml` in your working directory.

### 2) Configure Telegram + Storage

Open `config.toml` and set your Telegram token and a storage target:

```toml
lang = "en"
workers = 3
retry = 3
threads = 4
stream = false

[telegram]
token = "YOUR_BOT_TOKEN"

[[storages]]
name = "Local-1"
type = "local"
base_path = "./downloads"
```

### 3) Start the bot

```bash
./teleload
```

### 4) Use in Telegram 💬

Send supported URLs or files to your bot chat. Teleload will:

1. Parse the link/file.
2. Create a task with retries.
3. Download using yt-dlp/aria2 when relevant.
4. Save to the storage backend you configured.
5. Update progress in the same Telegram message.

### 5) Advanced usage tips ⚙️

- **Batch URLs**: Send multiple links in one message to create a batch task.
- **Storage routing**: Use rules in `config.toml` to route content to different storages.
- **Plugins**: Add JS parsers in `plugins/` for custom websites and formats.
- **Performance**: Increase `workers` and `threads` to scale throughput.
- **HLS power mode**: HLS streams benefit from native concurrent fragments (see section above).

## Build, Test, and Lint

```bash
# Build
go build -o Teleload .

# Run tests
go test ./...

# Format and lint
go fmt ./...
go vet ./...
```

## Security & Best Practices

- 🔐 Use a dedicated Telegram bot token and API credentials.
- 👥 Restrict bot access with user allowlists/denylists.
- 📁 Store secrets outside version control.
- 🌐 Run behind a proxy if needed.

## Roadmap-Ready

Teleload is structured for enterprise-grade evolution:

## Downloader Audit & Roadmap

For a detailed audit of downloader-related modules, duplicate/legacy surfaces, and a prioritized roadmap of reliability features (resume, segmentation, checksum, QoS, CLI flags), see:

- `docs/downloader_audit.md`

- Horizontal scaling through task queues.
- Advanced metadata pipelines.
- More storage backends and parser plugins.
- Enhanced observability and alerting.

## Contributing

Contributions are welcome. Please review the `docs/content/en/contribute/` guide for workflows, standards, and development conventions.

## License

MIT License. See `LICENSE`.
