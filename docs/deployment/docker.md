# Docker Deployment

SeedReap provides official Docker images based on distroless for minimal attack surface.

## Quick Start

```bash
docker run -d \
  --name seedreap \
  -v /path/to/config.yaml:/config/config.yaml:ro \
  -v /path/to/ssh:/config/ssh:ro \
  -v /downloads:/downloads \
  -p 8423:8423 \
  ghcr.io/seedreap/seedreap:latest
```

## Docker Compose

```yaml title="docker-compose.yml"
version: "3.8"

services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    container_name: seedreap
    ports:
      - "8423:8423"
    volumes:
      - ./config.yaml:/config/config.yaml:ro
      - ./ssh:/config/ssh:ro
      - /downloads:/downloads
    restart: unless-stopped
```

## Volume Mounts

| Path                  | Description           | Mode       |
| --------------------- | --------------------- | ---------- |
| `/config/config.yaml` | Configuration file    | Read-only  |
| `/config/ssh/`        | SSH keys directory    | Read-only  |
| `/downloads`          | Downloads destination | Read-write |

## SSH Key Permissions

SSH keys must have correct permissions. In your compose file or run command:

```yaml
volumes:
  - type: bind
    source: ./ssh
    target: /config/ssh
    read_only: true
```

Ensure your local SSH key has restrictive permissions:

```bash
chmod 600 ./ssh/id_ed25519
```

## Environment Variables

Override configuration with environment variables:

```yaml title="docker-compose.yml"
services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    environment:
      - SEEDREAP_SERVER_LISTEN=[::]:8423
      - SEEDREAP_SYNC_MAX_CONCURRENT=4
    # ...
```

## Health Checks

Add a health check to your compose file:

```yaml title="docker-compose.yml"
services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    healthcheck:
      test: ["CMD", "/usr/local/bin/seedreap", "health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    # ...
```

## Full Example

```yaml title="docker-compose.yml"
version: "3.8"

services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    container_name: seedreap
    ports:
      - "8423:8423"
    volumes:
      - ./config.yaml:/config/config.yaml:ro
      - ./ssh:/config/ssh:ro
      - /data/downloads:/downloads
    environment:
      - TZ=America/New_York
    healthcheck:
      test: ["CMD", "/usr/local/bin/seedreap", "health"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

## Networking

### With Sonarr/Radarr

If running Sonarr/Radarr in Docker, use Docker networking:

```yaml title="docker-compose.yml"
version: "3.8"

services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    networks:
      - media

  sonarr:
    image: linuxserver/sonarr
    networks:
      - media

  radarr:
    image: linuxserver/radarr
    networks:
      - media

networks:
  media:
    driver: bridge
```

Then use service names in your config:

```yaml title="config.yaml"
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    # ...
```

## Updating

```bash
docker compose pull
docker compose up -d
```
