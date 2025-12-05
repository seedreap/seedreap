# Quick Start

This guide will get you up and running with SeedReap in just a few minutes.

## 1. Create Configuration

Create a configuration file at one of these locations:

- `~/.seedreap.yaml`
- `./config.yaml`
- `/config/config.yaml`

```yaml title="config.yaml"
server:
  listen: "[::]:8423"

sync:
  downloads_path: /downloads        # Where synced files end up
  syncing_path: /downloads/syncing  # Temporary staging area
  max_concurrent: 2                 # Files to sync at once
  parallel_connections: 8           # Multi-threaded streams per file
  poll_interval: 30s

downloaders:
  seedbox:
    type: qbittorrent
    url: http://your-seedbox:8080
    username: admin
    password: your-password
    ssh:
      host: your-seedbox
      port: 22
      user: your-user
      key_file: /path/to/ssh/key

apps:
  sonarr:
    type: sonarr
    url: http://localhost:8989
    api_key: your-sonarr-api-key
    category: tv-sonarr

  radarr:
    type: radarr
    url: http://localhost:7878
    api_key: your-radarr-api-key
    category: movies-radarr
```

## 2. Start SeedReap

=== "Binary"

```bash
seedreap
```

=== "Docker"

```bash
docker run -d \
  --name seedreap \
  -v $(pwd)/config.yaml:/config/config.yaml \
  -v ~/.ssh/seedbox_key:/config/ssh/id_ed25519:ro \
  -v /downloads:/downloads \
  -p 8423:8423 \
  ghcr.io/seedreap/seedreap:latest serve
```

## 3. Access the Web UI

Open your browser to [http://localhost:8423](http://localhost:8423) to view the dashboard.

## 4. Verify It's Working

Check the health endpoint:

```bash
curl http://localhost:8423/api/health
```

Expected response:

```json
{"status": "ok"}
```

View configured downloaders:

```bash
curl http://localhost:8423/api/downloaders
```

View configured apps:

```bash
curl http://localhost:8423/api/apps
```

## How It Works

1. SeedReap polls your qBittorrent instance for downloads matching configured categories
2. When files complete, they're transferred via SFTP using multi-threaded streams
3. Files are staged in `syncing_path`, then moved to `downloads_path/<downloader>/<category>/`
4. The appropriate app (Sonarr/Radarr) is notified to import the files

## Troubleshooting

### Enable Debug Logging

```bash
seedreap --log-level debug --log-pretty
```

### Test SSH Connection

```bash
ssh -i /path/to/key user@seedbox
```

## Next Steps

- [Configuration Reference](../configuration/index.md) - All configuration options
- [Docker Deployment](../deployment/docker.md) - Production Docker setup
- [Kubernetes Deployment](../deployment/kubernetes.md) - Deploy on Kubernetes
