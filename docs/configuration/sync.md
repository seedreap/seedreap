# Sync Settings

The sync section controls how SeedReap transfers files from your seedbox.

```yaml
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  maxConcurrent: 2
  parallelConnections: 8
  pollInterval: 30s
  transferSpeedMax: 0
```

## Options

| Option                | Type     | Default  | Description                                       |
| --------------------- | -------- | -------- | ------------------------------------------------- |
| `downloadsPath`       | string   | Required | Final destination for synced files                |
| `syncingPath`         | string   | Required | Temporary staging directory during transfer       |
| `maxConcurrent`       | int      | `2`      | Maximum files to transfer concurrently            |
| `parallelConnections` | int      | `8`      | Parallel connections per file transfer            |
| `pollInterval`        | duration | `30s`    | How often to check for new downloads              |
| `transferSpeedMax`    | int      | `0`      | Speed limit per file in bytes/sec (0 = unlimited) |

## downloadsPath

The final destination directory where synced files are placed. Files are organized by downloader and category:

```text
/downloads/
└── seedbox/
    ├── tv-sonarr/
    │   └── Show.S01E01/
    ├── movies-radarr/
    │   └── Movie.2024/
    └── misc/
        └── Other.Download/
```

This structure allows you to organize downloads by source when using multiple seedboxes.

## syncingPath

A temporary staging directory used during transfers. Files are transferred here first, then moved to
`downloadsPath` when complete. This ensures partial transfers don't trigger imports.

!!! tip "Same Filesystem"
    Keep `syncingPath` on the same filesystem as `downloadsPath` for instant atomic moves instead of copies.

## maxConcurrent

Controls how many files can be transferred simultaneously. Higher values increase overall throughput but use
more bandwidth and system resources.

```yaml
sync:
  maxConcurrent: 4  # Transfer 4 files at once
```

## parallelConnections

Each file can be downloaded using multiple parallel connections (segments). This dramatically increases speed
for large files over high-latency connections.

```yaml
sync:
  parallelConnections: 16  # 16 parallel connections per file
```

!!! note "Server Limits"
    Some servers limit concurrent connections. If transfers fail, try reducing this value.

Recommended values:

| Connection Type           | Recommended |
| ------------------------- | ----------- |
| Low latency (<50ms)       | 4-8         |
| Medium latency (50-150ms) | 8-16        |
| High latency (>150ms)     | 16-32       |

## pollInterval

How frequently SeedReap checks download clients for completed files.

```yaml
sync:
  pollInterval: 15s  # Check every 15 seconds
```

Shorter intervals mean faster detection but more API calls to your download client.

## transferSpeedMax

Limit the transfer speed **per file** in bytes per second. Set to `0` for unlimited.

!!! important "Speed Limit is Per File"
    The speed limit applies to each concurrent file transfer independently.
    **Total maximum bandwidth = `transferSpeedMax` × `maxConcurrent`**
    Example: With `transferSpeedMax: 10485760` (10 MB/s) and `maxConcurrent: 2`, the total maximum bandwidth is 20 MB/s.

```yaml
sync:
  transferSpeedMax: 52428800  # 50 MB/s per file
  maxConcurrent: 2            # Total max: 100 MB/s
```

Common values:

| Speed    | Bytes/sec     |
| -------- | ------------- |
| 10 MB/s  | `10485760`    |
| 25 MB/s  | `26214400`    |
| 50 MB/s  | `52428800`    |
| 100 MB/s | `104857600`   |

## Example Configurations

### High-Speed Home Server

```yaml
sync:
  downloadsPath: /data/downloads
  syncingPath: /data/downloads/.syncing
  maxConcurrent: 4
  parallelConnections: 16
  pollInterval: 15s
  transferSpeedMax: 0
```

### Limited Bandwidth

```yaml
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  maxConcurrent: 2
  parallelConnections: 4
  pollInterval: 60s
  transferSpeedMax: 5242880  # 5 MB/s per file = 10 MB/s total max
```
