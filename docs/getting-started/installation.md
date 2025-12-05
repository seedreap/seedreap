# Installation

## Requirements

- **SSH access** to your seedbox
- A supported download client (qBittorrent)

## Install SeedReap

### From Source

```bash
go install github.com/seedreap/seedreap@latest
```

### Docker

```bash
docker pull ghcr.io/seedreap/seedreap:latest
```

### Build from Source

```bash
git clone https://github.com/seedreap/seedreap.git
cd seedreap
go build -o seedreap .
```

## SSH Key Setup

SeedReap uses SSH to connect to your seedbox for file transfers. Generate an SSH key pair if you don't have one:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/seedbox_key -N ""
```

Copy the public key to your seedbox:

```bash
ssh-copy-id -i ~/.ssh/seedbox_key.pub user@seedbox
```

Test the connection:

```bash
ssh -i ~/.ssh/seedbox_key user@seedbox
```

## Next Steps

- [Quick Start Guide](quickstart.md) - Get up and running in minutes
- [Configuration Overview](../configuration/index.md) - Detailed configuration options
