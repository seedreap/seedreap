# Web UI

SeedReap includes a web-based user interface for monitoring sync status and managing downloads.

## Status Icons

The status icon in the top-left of the transfer status bar indicates the current overall state:

| Icon        | State    | Description                              |
| ----------- | -------- | ---------------------------------------- |
| ⬇ (pulsing) | Syncing  | Actively transferring files from seedbox |
| ✓           | Complete | All tracked files have been synced       |
| ⏳           | Waiting  | Downloads are in progress on seedbox     |
| ⏸           | Paused   | Downloads are paused on seedbox          |
| ●           | Idle     | Monitoring for new downloads             |

Hover over the icon to see a tooltip with more details.

## Download States

Each tracked download shows a status badge indicating its current state:

| Status      | Color  | Description                                             |
| ----------- | ------ | ------------------------------------------------------- |
| DOWNLOADING | Blue   | Torrent is downloading on the seedbox                   |
| PAUSED      | Orange | Torrent is paused/stopped on the seedbox                |
| DISCOVERED  | Cyan   | Download found, waiting for files to complete           |
| PENDING     | Gray   | Ready to sync, waiting in queue                         |
| SYNCING     | Yellow | Files are being transferred from seedbox                |
| IMPORTING   | Purple | Files synced, triggering import in app (Sonarr/Radarr)  |
| COMPLETE    | Green  | Fully synced and imported                               |
| ERROR       | Red    | An error occurred during sync or import                 |

Hover over any status badge to see a tooltip with more details.

## Transfer Status Bar

The transfer status bar at the top shows:

- **Files**: Number of files synced / total files tracked
- **Transfer Rate**: Current sync speed (or "Idle" when not syncing)
- **Progress**: Bytes synced / total bytes to sync
- **ETA**: Estimated time to complete current sync operations

Note: The progress bar only counts files that are ready to sync or actively syncing. Torrents still downloading
on the seedbox are not included in the progress calculation.

## Stats Cards

The stats cards show counts of downloads in various states:

- **Total Tracked**: All downloads being monitored
- **Downloading**: Torrents still downloading on seedbox
- **Syncing**: Downloads actively being synced
- **Complete**: Downloads fully processed
- **Errors**: Downloads with errors

## Sync Jobs Table

The main table shows all tracked downloads with:

- **Name**: Download name (click to expand and see individual files)
- **Downloader**: Which seedbox the download is from
- **Category**: The category/label from the download client
- **Files**: Number of files in the download
- **Status**: Current state (see Download States above)
- **Progress**: Visual progress bar
  - Blue: Seedbox download progress (for downloading torrents)
  - Yellow: Sync progress (for syncing torrents)
  - Green: Complete
- **Size**: Bytes transferred / total size

Click on any row to expand and see individual file progress.
