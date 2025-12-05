# API Reference

SeedReap provides a RESTful API for monitoring and integration.

## Base URL

```text
http://localhost:8423/api
```

## Endpoints

### Health Check

Check if the server is running.

```http
GET /api/health
```

**Response**

```json
{
  "status": "ok"
}
```

---

### Statistics

Get orchestrator statistics.

```http
GET /api/stats
```

**Response**

```json
{
  "total_tracked": 5,
  "downloading": 1,
  "syncing": 2,
  "complete": 2
}
```

---

### List Downloads

Get all tracked downloads with transfer status.

```http
GET /api/downloads
```

**Response**

```json
[
  {
    "id": "abc123",
    "name": "Show.S01E01.720p",
    "downloader": "seedbox",
    "category": "tv-sonarr",
    "app": "sonarr",
    "state": "syncing",
    "progress": 100,
    "size": 1073741824,
    "completed_size": 536870912,
    "total_files": 1,
    "bytes_per_sec": 52428800,
    "discovered_at": "2024-01-15T10:30:00Z"
  }
]
```

| Field            | Type   | Description                        |
| ---------------- | ------ | ---------------------------------- |
| `id`             | string | Unique download identifier         |
| `name`           | string | Download name                      |
| `downloader`     | string | Source downloader name             |
| `category`       | string | Download category                  |
| `app`            | string | Target app name                    |
| `state`          | string | Current state (see below)          |
| `progress`       | float  | Download progress (0-100)          |
| `size`           | int    | Total size in bytes                |
| `completed_size` | int    | Bytes transferred so far           |
| `total_files`    | int    | Number of files in download        |
| `bytes_per_sec`  | int    | Current transfer speed             |
| `error`          | string | Error message if failed (optional) |
| `discovered_at`  | string | ISO 8601 timestamp                 |
| `completed_at`   | string | ISO 8601 timestamp (optional)      |

**States**

- `discovered` - Just found in downloader
- `syncing` - Files being transferred
- `synced` - All files transferred
- `moving` - Moving from staging to final location
- `importing` - Triggering app import
- `complete` - Fully processed
- `error` - Error occurred

---

### Get Download

Get details for a specific download including per-file progress.

```http
GET /api/downloaders/:downloader/downloads/:id
```

**Parameters**

| Parameter    | Type   | Description              |
| ------------ | ------ | ------------------------ |
| `downloader` | string | Downloader name          |
| `id`         | string | Download ID              |

**Response**

```json
{
  "id": "abc123",
  "name": "Show.S01E01.720p",
  "downloader": "seedbox",
  "category": "tv-sonarr",
  "state": "syncing",
  "progress": 100,
  "size": 1073741824,
  "save_path": "/downloads/tv-sonarr",
  "completed_size": 536870912,
  "total_files": 1,
  "bytes_per_sec": 52428800,
  "remote_base": "/home/user/downloads/Show.S01E01.720p",
  "local_base": "/downloads/syncing/seedbox/abc123",
  "final_path": "/downloads/tv-sonarr/Show.S01E01.720p",
  "files": [
    {
      "path": "Show.S01E01.720p.mkv",
      "size": 1073741824,
      "transferred": 536870912,
      "status": "syncing",
      "bytes_per_sec": 52428800
    }
  ]
}
```

---

### Speed History

Get transfer speed history for sparkline visualization.

```http
GET /api/speed-history
```

**Response**

```json
[
  {"speed": 0, "timestamp": 1705312200},
  {"speed": 52428800, "timestamp": 1705312201},
  {"speed": 51234567, "timestamp": 1705312202},
  {"speed": 53456789, "timestamp": 1705312203},
  {"speed": 50000000, "timestamp": 1705312204}
]
```

Returns an array of speed samples, each containing:

| Field       | Type | Description                        |
| ----------- | ---- | ---------------------------------- |
| `speed`     | int  | Transfer speed in bytes per second |
| `timestamp` | int  | Unix timestamp of the sample       |

---

### List Downloaders

Get configured downloaders.

```http
GET /api/downloaders
```

**Response**

```json
[
  {
    "name": "seedbox",
    "type": "qbittorrent"
  }
]
```

---

### List Apps

Get configured apps.

```http
GET /api/apps
```

**Response**

```json
[
  {
    "name": "sonarr",
    "type": "sonarr",
    "category": "tv-sonarr"
  },
  {
    "name": "radarr",
    "type": "radarr",
    "category": "movies-radarr"
  }
]
```

## Error Responses

Errors return appropriate HTTP status codes with a JSON body:

```json
{
  "error": "download not found"
}
```

| Status | Meaning               |
| ------ | --------------------- |
| 400    | Bad request           |
| 404    | Resource not found    |
| 500    | Internal server error |
