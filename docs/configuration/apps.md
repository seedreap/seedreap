# Apps

Apps are applications that process synced downloads. Each app is associated with a download category and is
notified when files finish syncing.

## *arr Apps (Sonarr, Radarr)

SeedReap integrates with the [*arr](https://wiki.servarr.com/) family of applications. Currently supported:

- [Sonarr](https://sonarr.tv/) - TV show management
- [Radarr](https://radarr.video/) - Movie management

All *arr apps share the same configuration options and behavior. When a download completes, SeedReap triggers
a scan command via the app's API.

### Configuration

```yaml
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    api_key: your-sonarr-api-key
    category: tv-sonarr
    downloads_path: /downloads/tv  # Optional override

  radarr:
    type: radarr
    url: http://radarr:7878
    api_key: your-radarr-api-key
    category: movies-radarr
```

### Options

| Option                       | Type   | Required | Description                                                       |
| ---------------------------- | ------ | -------- | ----------------------------------------------------------------- |
| `type`                       | string | Yes      | `sonarr` or `radarr`                                              |
| `url`                        | string | Yes      | URL to the *arr instance                                          |
| `api_key`                    | string | Yes      | API key for authentication                                        |
| `category`                   | string | Yes      | Download category to match                                        |
| `downloads_path`             | string | No       | Override destination path                                         |
| `cleanup_on_category_change` | bool   | No       | Delete synced files when category changes (default: false)        |
| `cleanup_on_remove`          | bool   | No       | Delete synced files when removed from downloader (default: false) |

### Getting Your API Key

1. Open the *arr application
2. Go to Settings > General
3. Copy the API Key

### Default Ports

| App    | Default Port |
| ------ | ------------ |
| Sonarr | 8989         |
| Radarr | 7878         |

### Setting Up Your *arr App

For SeedReap to work correctly with your *arr application, you need to configure two important settings:

#### Remote Path Mapping

Since SeedReap syncs files from your seedbox to a local path, you need to configure a Remote Path Mapping in
your *arr app so it knows where to find the imported files.

1. Go to **Settings > Download Clients**
2. Scroll to **Remote Path Mappings**
3. Add a mapping:
   - **Host**: Your download client's hostname (e.g., `seedbox.example.com`)
   - **Remote Path**: The path on the seedbox (e.g., `/home/user/downloads/`)
   - **Local Path**: Where SeedReap syncs files to (e.g., `/downloads/seedbox/tv-sonarr/`)

This tells the *arr app to look for files in the local synced location instead of trying to access the remote seedbox path.

#### Post-Import Category (Recommended)

Configure your download client in the *arr app to change the torrent's category after import. This allows:

- SeedReap to detect the import completed and stop monitoring the download
- Automatic cleanup of synced files (with `cleanup_on_category_change: true`)
- The torrent to continue seeding on your seedbox without SeedReap interference

To set this up:

1. Go to **Settings > Download Clients**
2. Edit your download client (e.g., qBittorrent)
3. Set **Post-Import Category** to a different category (e.g., `tv-imported` or `seeding`)
4. In SeedReap, enable cleanup:

```yaml
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    api_key: your-api-key
    category: tv-sonarr
    cleanup_on_category_change: true  # Clean up after import
```

With this setup:

1. Torrent downloads to seedbox with category `tv-sonarr`
2. SeedReap syncs files to local storage
3. Sonarr imports and moves files to your media library
4. Sonarr changes category to `tv-imported`
5. SeedReap detects the change and deletes the local synced copy
6. Torrent continues seeding on seedbox (original files untouched)

## Passthrough

The passthrough app syncs files without triggering any API calls. Use this for:

- Categories not associated with any app
- Apps that aren't yet supported
- Manual processing workflows

```yaml
apps:
  misc:
    type: passthrough
    category: misc
    downloads_path: /downloads/misc  # Optional
```

### Options

| Option                       | Type   | Required | Description                                                       |
| ---------------------------- | ------ | -------- | ----------------------------------------------------------------- |
| `type`                       | string | Yes      | Must be `passthrough`                                             |
| `category`                   | string | Yes      | Download category to match                                        |
| `downloads_path`             | string | No       | Override destination path                                         |
| `cleanup_on_category_change` | bool   | No       | Delete synced files when category changes (default: false)        |
| `cleanup_on_remove`          | bool   | No       | Delete synced files when removed from downloader (default: false) |

## Multiple Apps per Category

You can configure multiple apps for the same category. All matching apps will be notified when a download completes:

```yaml
apps:
  sonarr-hd:
    type: sonarr
    url: http://sonarr-hd:8989
    api_key: api-key-1
    category: tv

  sonarr-4k:
    type: sonarr
    url: http://sonarr-4k:8989
    api_key: api-key-2
    category: tv
```

## Download Paths

By default, synced files are placed in:

```text
{sync.downloads_path}/{downloader_name}/{category}/{download_name}/
```

For example, with this configuration:

```yaml
sync:
  downloads_path: /downloads

downloaders:
  seedbox:
    type: qbittorrent
    # ...

apps:
  sonarr:
    type: sonarr
    category: tv-sonarr
```

A download named "Show.S01E01" from the "seedbox" downloader would be synced to:

```text
/downloads/seedbox/tv-sonarr/Show.S01E01/
```

This structure allows you to organize downloads by source when using multiple seedboxes.

### Custom Paths

Override the default path with `downloads_path`:

```yaml
apps:
  sonarr:
    type: sonarr
    category: tv-sonarr
    downloads_path: /media/tv/incoming
```

## Category Change and Removal Behavior

SeedReap monitors for category changes and removals in the download client and handles them intelligently.

### Category Changes

When a download's category changes in qBittorrent:

1. **To another tracked app**: Files are automatically moved to the new app's downloads path and the new app's
   import is triggered. For example, if a download changes from `tv-sonarr` to `movies-radarr`, the files are
   moved to Radarr's download path and Radarr is notified. If the download is still syncing, the sync continues
   to the new app's path instead of being cancelled.

2. **To an untracked category**: If `cleanup_on_category_change` is enabled, files are deleted. Otherwise, they
   remain in place. If still syncing, the sync is cancelled and all files are cleaned up.

3. **While still syncing to an untracked category**: The sync is cancelled, staging files are cleaned up, and
   any partially synced files are removed.

### Removal from Downloader

When a torrent is removed from qBittorrent:

1. **If complete**: Files are cleaned up if `cleanup_on_remove` is enabled.

2. **While still syncing**: The sync is cancelled and all files (staging and final) are cleaned up automatically.

## Cleanup Options

These options control whether to delete synced files when certain events occur.

### Post-Import Category Change

Many *arr apps support a "Post-Import Category" setting that changes the torrent's category after import. When
this happens, SeedReap detects the change. If the new category doesn't match another app, you can enable cleanup:

```yaml
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    api_key: your-api-key
    category: tv-sonarr
    cleanup_on_category_change: true  # Delete synced files when category changes to untracked category
```

### Removal from Downloader

If a torrent is removed from the download client (e.g., after seeding completes), SeedReap can clean up the synced files:

```yaml
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    api_key: your-api-key
    category: tv-sonarr
    cleanup_on_remove: true  # Delete synced files when removed from downloader
```

### Important Notes

- Both options default to `false` to prevent accidental data loss
- Cleanup options only apply to completed downloads - incomplete syncs are always cleaned up when cancelled
- When a download changes to another tracked app's category, files are moved (not deleted) regardless of cleanup settings
- When a download is cleaned up or moved, it is removed from SeedReap's tracking
- If you use hardlinks in your *arr app, the synced files are just copies and can be safely deleted after import
