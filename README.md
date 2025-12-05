<h1 align="center"><img src="images/logo.png" alt="SeedReap Logo" width="200"></h1>

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

Create a config file based on the [example configuration](config.example.yaml):

```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your settings
```

Alternatively, SeedReap can be configured entirely using environment variables - no config file needed.
See [Environment Variables](docs/configuration/environment-variables.md) for details.

### 3. Run

```bash
seedreap serve --config config.yaml
```

The web UI will be available at `http://localhost:8423`.

## Documentation

| Topic                                                                | Description                        |
| -------------------------------------------------------------------- | ---------------------------------- |
| [Installation](docs/getting-started/installation.md)                 | Detailed installation instructions |
| [Quick Start](docs/getting-started/quickstart.md)                    | Get up and running quickly         |
| [Configuration](docs/configuration/index.md)                         | Full configuration reference       |
| [Environment Variables](docs/configuration/environment-variables.md) | Configure entirely via env vars    |
| [Apps (Sonarr/Radarr)](docs/configuration/apps.md)                   | Setting up *arr app integration    |
| [Downloaders](docs/configuration/downloaders.md)                     | Configuring download clients       |
| [Web UI](docs/ui.md)                                                 | Understanding the dashboard        |
| [API Reference](docs/api.md)                                         | REST API documentation             |
| [Docker Deployment](docs/deployment/docker.md)                       | Running with Docker                |
| [Kubernetes Deployment](docs/deployment/kubernetes.md)               | Kubernetes/Helm setup              |
| [Extending](docs/development/extending.md)                           | Adding new downloaders or apps     |

## Requirements

- **SSH access** - To your seedbox for file transfers
- **qBittorrent** - (or other supported download client) with Web UI enabled

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Acknowledgments

- [seedsync](https://github.com/ipsingh06/seedsync) - The original inspiration for this project
- [Sonarr](https://sonarr.tv/) / [Radarr](https://radarr.video/) - Media management applications
- [rclone](https://rclone.org/) - Cloud storage sync tool (via Go libraries)
