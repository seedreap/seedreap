# Downloaders

Downloaders are the download clients that SeedReap monitors for completed downloads. Each downloader is
configured with a unique name and type-specific settings.

## qBittorrent

The qBittorrent downloader connects to a qBittorrent instance via its Web API and uses SSH/SFTP for file
transfers.

```yaml
downloaders:
  seedbox:  # Unique name for this downloader
    type: qbittorrent
    url: http://seedbox:8080
    username: admin
    password: your-password
    ssh:
      host: seedbox
      port: 22
      user: qbittorrent
      key_file: /config/ssh/id_ed25519
```

### Options

| Option         | Type   | Required | Description                     |
| -------------- | ------ | -------- | ------------------------------- |
| `type`         | string | Yes      | Must be `qbittorrent`           |
| `url`          | string | Yes      | URL to qBittorrent Web UI       |
| `username`     | string | Yes      | qBittorrent username            |
| `password`     | string | Yes      | qBittorrent password            |
| `ssh.host`     | string | Yes      | SSH hostname for SFTP transfers |
| `ssh.port`     | int    | No       | SSH port (default: 22)          |
| `ssh.user`     | string | Yes      | SSH username                    |
| `ssh.key_file` | string | Yes      | Path to SSH private key         |

### Category Matching

SeedReap only syncs downloads that match a configured app's category. Set the category in qBittorrent to match
your app configuration:

- Sonarr category: `tv-sonarr`
- Radarr category: `movies-radarr`

### Multiple Seedboxes

You can configure multiple downloaders to sync from multiple seedboxes:

```yaml
downloaders:
  seedbox-eu:
    type: qbittorrent
    url: http://eu-seedbox:8080
    username: admin
    password: password1
    ssh:
      host: eu-seedbox
      user: user
      key_file: /config/ssh/eu_key

  seedbox-us:
    type: qbittorrent
    url: http://us-seedbox:8080
    username: admin
    password: password2
    ssh:
      host: us-seedbox
      user: user
      key_file: /config/ssh/us_key
```

## SSH Key Setup

Generate an SSH key for SeedReap to use:

```bash
ssh-keygen -t ed25519 -f seedreap_key -N ""
```

Copy the public key to your seedbox:

```bash
ssh-copy-id -i seedreap_key.pub user@seedbox
```

!!! warning "Key Permissions"
    Ensure the private key has restrictive permissions:
    ```bash
    chmod 600 /config/ssh/id_ed25519
    ```
