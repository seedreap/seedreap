# Environment Variables

SeedReap can be configured entirely using environment variables, making it ideal for containerized deployments
where you may not want to mount a configuration file.

All environment variables use the `SEEDREAP_` prefix. Nested configuration keys use underscores (`_`) as separators.

## Quick Reference

### Server

| Environment Variable     | Config Key      | Default      | Description                      |
| ------------------------ | --------------- | ------------ | -------------------------------- |
| `SEEDREAP_SERVER_LISTEN` | `server.listen` | `[::]:8423`  | Address and port for HTTP server |

### Sync Settings

| Environment Variable                | Config Key                 | Default              | Description                                                                      |
| ----------------------------------- | -------------------------- | -------------------- | -------------------------------------------------------------------------------- |
| `SEEDREAP_SYNC_DOWNLOADSPATH`       | `sync.downloadsPath`       | `/downloads`         | Final destination for synced files                                               |
| `SEEDREAP_SYNC_SYNCINGPATH`         | `sync.syncingPath`         | `/downloads/syncing` | Temporary staging directory                                                      |
| `SEEDREAP_SYNC_MAXCONCURRENT`       | `sync.maxConcurrent`       | `2`                  | Maximum concurrent file transfers                                                |
| `SEEDREAP_SYNC_PARALLELCONNECTIONS` | `sync.parallelConnections` | `8`                  | Parallel connections per file                                                    |
| `SEEDREAP_SYNC_POLLINTERVAL`        | `sync.pollInterval`        | `30s`                | How often to poll download clients                                               |
| `SEEDREAP_SYNC_TRANSFERSPEEDMAX`    | `sync.transferSpeedMax`    | `0`                  | Speed limit per file (bytes/sec, 0=unlimited). Total max = this × maxConcurrent  |

### Downloaders

To configure downloaders via environment variables, you must first declare which downloaders exist using
`SEEDREAP_DOWNLOADERS` as a comma-separated list.

| Environment Variable   | Description                                           |
| ---------------------- | ----------------------------------------------------- |
| `SEEDREAP_DOWNLOADERS` | Comma-separated list of downloader names to configure |

For each downloader named `{name}` (case-sensitive, supports hyphens):

| Environment Variable                      | Config Key                       | Required | Description                           |
| ----------------------------------------- | -------------------------------- | -------- | ------------------------------------- |
| `SEEDREAP_DOWNLOADERS_{NAME}_TYPE`        | `downloaders.{name}.type`        | Yes      | Downloader type (e.g., `qbittorrent`) |
| `SEEDREAP_DOWNLOADERS_{NAME}_URL`         | `downloaders.{name}.url`         | Yes      | URL to download client                |
| `SEEDREAP_DOWNLOADERS_{NAME}_USERNAME`    | `downloaders.{name}.username`    | No       | Username for authentication           |
| `SEEDREAP_DOWNLOADERS_{NAME}_PASSWORD`    | `downloaders.{name}.password`    | No       | Password for authentication           |
| `SEEDREAP_DOWNLOADERS_{NAME}_SSH_HOST`    | `downloaders.{name}.ssh.host`    | Yes      | SSH hostname for transfers            |
| `SEEDREAP_DOWNLOADERS_{NAME}_SSH_PORT`    | `downloaders.{name}.ssh.port`    | No       | SSH port (default: 22)                |
| `SEEDREAP_DOWNLOADERS_{NAME}_SSH_USER`    | `downloaders.{name}.ssh.user`    | Yes      | SSH username                          |
| `SEEDREAP_DOWNLOADERS_{NAME}_SSH_KEYFILE` | `downloaders.{name}.ssh.keyFile` | Yes      | Path to SSH private key               |

### Apps

To configure apps via environment variables, you must first declare which apps exist using `SEEDREAP_APPS` as
a comma-separated list.

| Environment Variable | Description                                    |
| -------------------- | ---------------------------------------------- |
| `SEEDREAP_APPS`      | Comma-separated list of app names to configure |

For each app named `{name}` (case-sensitive, supports hyphens):

| Environment Variable                           | Config Key                            | Required | Description                                      |
| ---------------------------------------------- | ------------------------------------- | -------- | ------------------------------------------------ |
| `SEEDREAP_APPS_{NAME}_TYPE`                    | `apps.{name}.type`                    | Yes      | App type (`sonarr`, `radarr`, `passthrough`)     |
| `SEEDREAP_APPS_{NAME}_URL`                     | `apps.{name}.url`                     | For *arr | URL to *arr instance                             |
| `SEEDREAP_APPS_{NAME}_APIKEY`                  | `apps.{name}.apiKey`                  | For *arr | API key for authentication                       |
| `SEEDREAP_APPS_{NAME}_CATEGORY`                | `apps.{name}.category`                | Yes      | Download category to match                       |
| `SEEDREAP_APPS_{NAME}_DOWNLOADSPATH`           | `apps.{name}.downloadsPath`           | No       | Override destination path                        |
| `SEEDREAP_APPS_{NAME}_CLEANUPONCATEGORYCHANGE` | `apps.{name}.cleanupOnCategoryChange` | No       | Delete files on category change (`true`/`false`) |
| `SEEDREAP_APPS_{NAME}_CLEANUPONREMOVE`         | `apps.{name}.cleanupOnRemove`         | No       | Delete files when removed (`true`/`false`)       |

## Complete Example

Here's a complete example configuring SeedReap entirely via environment variables:

```bash
# Server
export SEEDREAP_SERVER_LISTEN="[::]:8423"

# Sync settings
export SEEDREAP_SYNC_DOWNLOADSPATH="/downloads"
export SEEDREAP_SYNC_SYNCINGPATH="/downloads/.seedreap-syncing"
export SEEDREAP_SYNC_MAXCONCURRENT="2"
export SEEDREAP_SYNC_PARALLELCONNECTIONS="8"
export SEEDREAP_SYNC_POLLINTERVAL="30s"

# Declare downloaders
export SEEDREAP_DOWNLOADERS="seedbox"

# Configure 'seedbox' downloader
export SEEDREAP_DOWNLOADERS_SEEDBOX_TYPE="qbittorrent"
export SEEDREAP_DOWNLOADERS_SEEDBOX_URL="http://seedbox.example.com:8080"
export SEEDREAP_DOWNLOADERS_SEEDBOX_USERNAME="admin"
export SEEDREAP_DOWNLOADERS_SEEDBOX_PASSWORD="secret123"
export SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_HOST="seedbox.example.com"
export SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_PORT="22"
export SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_USER="seeduser"
export SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_KEYFILE="/config/ssh/id_ed25519"

# Declare apps
export SEEDREAP_APPS="sonarr,radarr-4k,misc"

# Configure 'sonarr' app
export SEEDREAP_APPS_SONARR_TYPE="sonarr"
export SEEDREAP_APPS_SONARR_URL="http://sonarr:8989"
export SEEDREAP_APPS_SONARR_APIKEY="your-sonarr-api-key"
export SEEDREAP_APPS_SONARR_CATEGORY="tv-sonarr"
export SEEDREAP_APPS_SONARR_CLEANUPONCATEGORYCHANGE="true"

# Configure 'radarr-4k' app (note: hyphens are allowed in names)
export SEEDREAP_APPS_RADARR-4K_TYPE="radarr"
export SEEDREAP_APPS_RADARR-4K_URL="http://radarr-4k:7878"
export SEEDREAP_APPS_RADARR-4K_APIKEY="your-radarr-api-key"
export SEEDREAP_APPS_RADARR-4K_CATEGORY="movies-4k"
export SEEDREAP_APPS_RADARR-4K_CLEANUPONCATEGORYCHANGE="true"

# Configure 'misc' passthrough app
export SEEDREAP_APPS_MISC_TYPE="passthrough"
export SEEDREAP_APPS_MISC_CATEGORY="misc"
```

## Docker Compose Example

```yaml
services:
  seedreap:
    image: ghcr.io/seedreap/seedreap:latest
    environment:
      # Sync settings
      SEEDREAP_SYNC_DOWNLOADSPATH: /downloads
      SEEDREAP_SYNC_SYNCINGPATH: /downloads/.syncing
      SEEDREAP_SYNC_MAXCONCURRENT: "2"
      SEEDREAP_SYNC_PARALLELCONNECTIONS: "8"

      # Downloader
      SEEDREAP_DOWNLOADERS: seedbox
      SEEDREAP_DOWNLOADERS_SEEDBOX_TYPE: qbittorrent
      SEEDREAP_DOWNLOADERS_SEEDBOX_URL: http://seedbox:8080
      SEEDREAP_DOWNLOADERS_SEEDBOX_USERNAME: admin
      SEEDREAP_DOWNLOADERS_SEEDBOX_PASSWORD: ${SEEDBOX_PASSWORD}
      SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_HOST: seedbox.example.com
      SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_USER: seeduser
      SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_KEYFILE: /config/ssh/id_ed25519

      # Apps
      SEEDREAP_APPS: sonarr,radarr
      SEEDREAP_APPS_SONARR_TYPE: sonarr
      SEEDREAP_APPS_SONARR_URL: http://sonarr:8989
      SEEDREAP_APPS_SONARR_APIKEY: ${SONARR_API_KEY}
      SEEDREAP_APPS_SONARR_CATEGORY: tv-sonarr
      SEEDREAP_APPS_SONARR_CLEANUPONCATEGORYCHANGE: "true"
      SEEDREAP_APPS_RADARR_TYPE: radarr
      SEEDREAP_APPS_RADARR_URL: http://radarr:7878
      SEEDREAP_APPS_RADARR_APIKEY: ${RADARR_API_KEY}
      SEEDREAP_APPS_RADARR_CATEGORY: movies-radarr
      SEEDREAP_APPS_RADARR_CLEANUPONCATEGORYCHANGE: "true"
    volumes:
      - ./ssh:/config/ssh:ro
      - /downloads:/downloads
    ports:
      - "8423:8423"
```

## Kubernetes ConfigMap/Secret Example

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: seedreap-config
data:
  SEEDREAP_SYNC_DOWNLOADSPATH: /downloads
  SEEDREAP_SYNC_SYNCINGPATH: /downloads/.syncing
  SEEDREAP_SYNC_MAXCONCURRENT: "2"
  SEEDREAP_DOWNLOADERS: seedbox
  SEEDREAP_DOWNLOADERS_SEEDBOX_TYPE: qbittorrent
  SEEDREAP_DOWNLOADERS_SEEDBOX_URL: http://qbittorrent:8080
  SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_HOST: seedbox.example.com
  SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_USER: seeduser
  SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_KEYFILE: /config/ssh/id_ed25519
  SEEDREAP_APPS: sonarr,radarr
  SEEDREAP_APPS_SONARR_TYPE: sonarr
  SEEDREAP_APPS_SONARR_URL: http://sonarr:8989
  SEEDREAP_APPS_SONARR_CATEGORY: tv-sonarr
  SEEDREAP_APPS_RADARR_TYPE: radarr
  SEEDREAP_APPS_RADARR_URL: http://radarr:7878
  SEEDREAP_APPS_RADARR_CATEGORY: movies-radarr
---
apiVersion: v1
kind: Secret
metadata:
  name: seedreap-secrets
stringData:
  SEEDREAP_DOWNLOADERS_SEEDBOX_USERNAME: admin
  SEEDREAP_DOWNLOADERS_SEEDBOX_PASSWORD: your-password
  SEEDREAP_APPS_SONARR_APIKEY: your-sonarr-api-key
  SEEDREAP_APPS_RADARR_APIKEY: your-radarr-api-key
```

## Notes

### Naming Convention

- Environment variable names are **case-insensitive** for the config key portion, but the declared names in
  `SEEDREAP_DOWNLOADERS` and `SEEDREAP_APPS` are **case-sensitive** and used as-is for the map keys.
- Hyphens are allowed in downloader and app names (e.g., `radarr-4k`, `seedbox-eu`).
- The environment variable for a hyphenated name keeps the hyphen: `SEEDREAP_APPS_RADARR-4K_TYPE`.

### Precedence

When both a config file and environment variables are present:

1. Environment variables take precedence over config file values
2. Config file values take precedence over defaults

### Dynamic Maps

The `SEEDREAP_DOWNLOADERS` and `SEEDREAP_APPS` variables are special - they declare which map keys exist so
that the corresponding environment variables can be discovered. These list variables are processed and removed
before configuration is loaded.

### Boolean Values

Boolean environment variables accept: `true`, `false`, `1`, `0`, `yes`, `no` (case-insensitive).

### Duration Values

Duration values like `pollInterval` accept Go duration strings: `30s`, `1m`, `1h30m`, etc.
