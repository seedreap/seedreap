<h1 align="center"><img src="docs/images/logo.png" alt="SeedReap Logo" width="200"></h1>

# SeedReap

*Reap what your seedbox has sown.*

SeedReap syncs completed downloads from remote seedboxes to local storage using high-speed parallel SFTP
transfers (via rclone), then triggers media app imports.

> **Warning**
>
> This is early software under active development. APIs, configuration options, and behavior may change
> without notice. Use at your own risk.
>
> The web UI has been entirely built by [Claude](https://claude.ai).

## Why SeedReap?

If you run a seedbox and use *arr apps (Sonarr, Radarr, etc.), you need a way to get completed downloads from
your seedbox to your local media server. SeedReap solves this by:

- **Syncing files as they complete** - Don't wait for an entire season pack to finish; episodes sync as soon as they're
  done downloading
- **Maximizing transfer speeds** - Uses rclone with multi-threaded transfers to saturate your connection
- **Automating the workflow** - Triggers imports in your *arr apps automatically after sync
- **Cleaning up after itself** - Optionally removes synced files when the *arr app changes the torrent category post-import

## Features

- **High-Speed Parallel Transfers** - rclone with configurable multi-threaded streams per file
- **Incremental Sync** - Syncs individual files as they complete, even before the entire torrent finishes
- **Media App Integration** - Automatically triggers imports in Sonarr, Radarr, and other *arr apps
- **Smart Cleanup** - Detects category changes and can auto-cleanup synced files after import
- **Web UI** - Real-time dashboard with progress, transfer speeds, and status indicators
- **RESTful API** - Full API for integration and monitoring

## How It Works

```text
┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
│   qBittorrent   │─────▶│    SeedReap     │─────▶│     Sonarr      │
│   (Seedbox)     │ SFTP │   (Local)       │  API │     Radarr      │
└─────────────────┘      └─────────────────┘      └─────────────────┘
```

1. **Monitor** - Polls download clients for downloads in configured categories
2. **Sync** - Transfers completed files via SFTP (using rclone) to local staging
3. **Move** - Moves synced files to the final destination
4. **Import** - Triggers the appropriate *arr app to import

## Quick Start

### 1. Install

```bash
# From source
go install github.com/seedreap/seedreap@latest

# Or Docker
docker pull ghcr.io/seedreap/seedreap:latest
```

### 2. Configure

Create a config file based on the [example configuration](https://seedreap.github.io/seedreap/configuration/#example-configuration):

```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your settings
```

Alternatively, SeedReap can be configured entirely using environment variables - no config file needed.
See [Environment Variables](https://seedreap.github.io/seedreap/configuration/environment-variables/) for details.

### 3. Run

```bash
seedreap serve --config config.yaml
```

The web UI will be available at `http://localhost:8423`.

## Documentation

Full documentation is available at **[seedreap.github.io/seedreap](https://seedreap.github.io/seedreap/)**.

| Topic                 | Description                        |
| --------------------- | ---------------------------------- |
| [Installation]        | Detailed installation instructions |
| [Quick Start]         | Get up and running quickly         |
| [Configuration]       | Full configuration reference       |
| [Environment Vars]    | Configure entirely via env vars    |
| [Apps (Sonarr/Radarr)]| Setting up *arr app integration    |
| [Downloaders]         | Configuring download clients       |
| [Web UI]              | Understanding the dashboard        |
| [API Reference]       | REST API documentation             |
| [Docker Deployment]   | Running with Docker                |
| [Kubernetes]          | Kubernetes/Helm setup              |
| [Extending]           | Adding new downloaders or apps     |

[Installation]: https://seedreap.github.io/seedreap/getting-started/installation/
[Quick Start]: https://seedreap.github.io/seedreap/getting-started/quickstart/
[Configuration]: https://seedreap.github.io/seedreap/configuration/
[Environment Vars]: https://seedreap.github.io/seedreap/configuration/environment-variables/
[Apps (Sonarr/Radarr)]: https://seedreap.github.io/seedreap/configuration/apps/
[Downloaders]: https://seedreap.github.io/seedreap/configuration/downloaders/
[Web UI]: https://seedreap.github.io/seedreap/ui/
[API Reference]: https://seedreap.github.io/seedreap/api/
[Docker Deployment]: https://seedreap.github.io/seedreap/deployment/docker/
[Kubernetes]: https://seedreap.github.io/seedreap/deployment/kubernetes/
[Extending]: https://seedreap.github.io/seedreap/development/extending/

## Requirements

- **SSH access** - To your seedbox for file transfers
- **qBittorrent** - (or other supported download client) with Web UI enabled

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Acknowledgments

### Inspiration

- [seedsync](https://github.com/ipsingh06/seedsync) - The original inspiration for this project

### Web UI

- [Mithril.js](https://mithril.js.org/) - Lightweight JavaScript framework
- [DaisyUI](https://daisyui.com/) - Tailwind CSS component library
- [Tailwind CSS](https://tailwindcss.com/) - Utility-first CSS framework

### Go Libraries

- [rclone](https://rclone.org/) - Cloud storage sync (SFTP transfers)
- [Echo](https://echo.labstack.com/) - HTTP framework
- [Cobra](https://cobra.dev/) - CLI framework
- [Viper](https://github.com/spf13/viper) - Configuration management
- [zerolog](https://github.com/rs/zerolog) - Structured logging
