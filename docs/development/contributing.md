# Contributing

Thank you for your interest in contributing to SeedReap!

## Development Setup

### Prerequisites

- Go 1.21+
- Node.js 18+ (for UI development)
- Docker (optional)

### Clone the Repository

```bash
git clone https://github.com/seedreap/seedreap.git
cd seedreap
```

### Build

```bash
go build -o seedreap .
```

### Run Tests

```bash
go test ./...
```

### Run with Debug Logging

```bash
./seedreap --log-level debug --log-pretty
```

## Project Structure

```text
.
├── cmd/                    # CLI entry point and flags
├── internal/
│   ├── api/               # HTTP API server
│   ├── app/               # App integrations
│   │   ├── arrapp/        # Shared base for *arr apps
│   │   ├── sonarr/        # Sonarr client
│   │   ├── radarr/        # Radarr client
│   │   └── passthrough/   # No-op passthrough
│   ├── config/            # Configuration handling
│   ├── download/          # Download client integrations
│   ├── filesync/          # File sync job management
│   ├── orchestrator/      # Main orchestration logic
│   ├── server/            # Main application server
│   ├── testing/           # Reusable test mocks
│   └── transfer/          # Transfer backends (rclone)
├── ui/                    # Web UI (embedded)
└── docs/                  # Documentation
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Run `golangci-lint` before submitting

```bash
golangci-lint run
```

## Submitting Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run tests: `go test ./...`
5. Commit with clear messages
6. Push to your fork
7. Open a Pull Request

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation as needed
- Ensure CI passes

## Reporting Issues

When reporting issues, please include:

- SeedReap version
- Operating system
- Configuration (sanitized)
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs

## Feature Requests

Feature requests are welcome! Please open an issue describing:

- The problem you're trying to solve
- Your proposed solution
- Any alternatives you've considered
