# SaveAny-Bot

SaveAny-Bot is a production-grade Telegram bot that reliably captures files and messages from Telegram and the web, then delivers them to your preferred storage backends. It is designed for scale, observability, and extensibility, with a modular architecture that supports pluggable parsers, multiple storage targets, and robust automation workflows.

## Why SaveAny-Bot

**Purpose-built for serious workloads.** SaveAny-Bot is engineered for teams and power users who need dependable automation, consistent storage rules, and a clean operational footprint. It delivers a stable, secure, and extensible foundation that can grow from a single-instance deployment to complex, multi-user environments.

## Key Capabilities

- **Multi-source ingestion**: Save files and messages from Telegram and external websites.
- **Multi-backend storage**: Store to local disk, WebDAV, S3-compatible storage, AList, Rclone, or Telegram.
- **Parser plugins**: Extend URL handling with JavaScript-based parsers, plus built-in Go parsers.
- **Task orchestration**: Concurrent workers, retries, and progress tracking for reliable transfers.
- **Rules & automation**: Rule-based routing and automatic organization of files.
- **Power tools**: Aria2 and yt-dlp integrations for advanced download scenarios.
- **Operational safety**: Rate limiting, context-aware logging, and structured error handling.

## Architecture Overview

SaveAny-Bot is composed of clear, scalable modules:

- **Core task engine**: Orchestrates download, parsing, and transfer tasks.
- **Storage abstraction**: Uniform interface across local, WebDAV, S3, Telegram, and more.
- **Parser layer**: Built-in parsers plus a plugin system for custom sources.
- **Bot client**: Telegram bot handlers, command routing, and middleware.
- **Database layer**: SQLite-backed persistence with GORM.

## Getting Started

### Prerequisites

- Go **1.24.2**
- A Telegram Bot Token from [BotFather](https://t.me/botfather)
- Optional: Telegram API ID & Hash from [my.telegram.org](https://my.telegram.org)

### Quickstart (Binary)

```bash
# Build
go build -o saveany-bot .

# Run
./saveany-bot
```

### Quickstart (Docker)

```bash
docker compose up -d
```

## Configuration

SaveAny-Bot uses a `config.toml` file in the working directory. If it does not exist, the bot generates a default file on startup.

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

- Local filesystem
- WebDAV
- S3-compatible (AWS S3, MinIO)
- AList
- Rclone
- Telegram (re-upload to target chats)

## Parser Plugins

SaveAny-Bot supports JavaScript-based parser plugins. Each plugin declares metadata and implements `canHandle()` and `parse()` functions. This allows you to add new websites and workflows without modifying the core application.

Example plugins live in the `plugins/` directory.

## Operations & Monitoring

- **Structured logging**: Context-aware logging with `charmbracelet/log`.
- **Progress tracking**: Real-time progress updates in Telegram messages.
- **Retry strategy**: Configurable retries per task.
- **Concurrency control**: Worker pool and download threading limits.

## Build, Test, and Lint

```bash
# Build
go build -o saveany-bot .

# Run tests
go test ./...

# Format and lint
go fmt ./...
go vet ./...
```

## Security & Best Practices

- Use a dedicated Telegram bot token and API credentials.
- Restrict bot access with user allowlists/denylists.
- Store secrets outside version control.
- Run behind a proxy if needed.

## Roadmap-Ready

SaveAny-Bot is structured for enterprise-grade evolution:

- Horizontal scaling through task queues.
- Advanced metadata pipelines.
- More storage backends and parser plugins.
- Enhanced observability and alerting.

## Contributing

Contributions are welcome. Please review the `docs/content/en/contribute/` guide for workflows, standards, and development conventions.

## License

MIT License. See `LICENSE`.
