# Configuration Overview

SeedReap is configured via a YAML file or environment variables. The configuration file is searched for in
these locations (in order):

1. Path specified with `--config` flag
2. `./config.yaml`
3. `~/.seedreap.yaml`
4. `/config/config.yaml`

## Full Example

```yaml title="config.yaml"
server:
  listen: "[::]:8423"

sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  maxConcurrent: 2
  parallelConnections: 8
  pollInterval: 30s
  transferSpeedMax: 0  # per file, 0 = unlimited

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    username: admin
    password: your-password
    ssh:
      host: seedbox
      port: 22
      user: qbittorrent
      keyFile: /config/ssh/id_ed25519

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: your-sonarr-api-key
    category: tv-sonarr

  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: your-radarr-api-key
    category: movies-radarr

  misc:
    type: passthrough
    category: misc
```

## Environment Variables

SeedReap can be configured entirely using environment variables - no config file needed. All options use the
`SEEDREAP_` prefix with underscores for hierarchy:

```bash
SEEDREAP_SERVER_LISTEN="[::]:8423"
SEEDREAP_SYNC_DOWNLOADSPATH="/downloads"
SEEDREAP_SYNC_MAXCONCURRENT=4
```

See [Environment Variables](environment-variables.md) for complete documentation and examples.

## Configuration Sections

| Section                                           | Description                    |
| ------------------------------------------------- | ------------------------------ |
| [server](#server)                                 | HTTP server settings           |
| [authentication](#authentication)                 | Authentication guidance        |
| [sync](#sync)                                     | Transfer and sync settings     |
| [downloaders](downloaders.md)                     | Download client configurations |
| [apps](apps.md)                                   | App configurations             |
| [environment variables](environment-variables.md) | Complete env var reference     |

## Server

```yaml
server:
  listen: "[::]:8423"  # Address to bind the HTTP server
```

| Option   | Type   | Default      | Description                      |
| -------- | ------ | ------------ | -------------------------------- |
| `listen` | string | `[::]:8423`  | Address and port for HTTP server |

## Authentication

SeedReap does not include built-in authentication. The web UI and API are read-only and only display
the current status of downloads and transfers - no sensitive operations can be performed through them.

If you need to restrict access to the UI or API, place a reverse proxy in front of SeedReap that
handles authentication. Common options include:

- **Nginx** with basic auth or OAuth2 proxy
- **Traefik** with forward auth middleware
- **Caddy** with basicauth or forward_auth
- **Authelia** or **Authentik** for SSO

Example with Traefik and basic auth:

```yaml
services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.seedreap.rule=Host(`seedreap.example.com`)"
      - "traefik.http.routers.seedreap.middlewares=seedreap-auth"
      - "traefik.http.middlewares.seedreap-auth.basicauth.users=admin:$$apr1$$..."
```

## Sync

```yaml
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  maxConcurrent: 2
  parallelConnections: 8
  pollInterval: 30s
  transferSpeedMax: 0  # per file, total max = this * maxConcurrent
```

See [Sync Settings](sync.md) for detailed documentation.
