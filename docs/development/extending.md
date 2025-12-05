# Extending SeedReap

SeedReap is designed to be extensible. You can add support for new download clients and apps.

## Adding a New Downloader

Downloaders are clients that SeedReap polls for completed downloads. To add a new downloader:

### 1. Implement the Interface

Add a new file in `internal/download/` with your implementation. Providers must be private types exposed via
the interface. They implement the package's `configurable` interface to support shared options:

```go title="internal/download/mydownloader.go"
package download

import (
    "context"

    "github.com/rs/zerolog"

    "github.com/seedreap/seedreap/internal/config"
)

// mydownloaderClient is private - only exposed via Downloader interface
type mydownloaderClient struct {
    name   string
    logger zerolog.Logger
    // ... other fields
}

// setLogger implements configurable for shared options
func (c *mydownloaderClient) setLogger(logger zerolog.Logger) {
    c.logger = logger
}

// NewMydownloader returns the Downloader interface, not the concrete type
func NewMydownloader(name string, cfg config.DownloaderConfig, opts ...Option) Downloader {
    c := &mydownloaderClient{
        name:   name,
        logger: zerolog.Nop(),
    }

    for _, opt := range opts {
        opt(c)
    }

    return c
}

func (c *mydownloaderClient) Name() string {
    return c.name
}

func (c *mydownloaderClient) Type() string {
    return "mydownloader"
}

func (c *mydownloaderClient) Connect(ctx context.Context) error {
    // Establish connection to the download client
    return nil
}

func (c *mydownloaderClient) Close() error {
    // Clean up connections
    return nil
}

func (c *mydownloaderClient) ListDownloads(ctx context.Context, categories []string) ([]Download, error) {
    // Return downloads matching the given categories
    return nil, nil
}

func (c *mydownloaderClient) GetDownload(ctx context.Context, id string) (*Download, error) {
    // Get a specific download by ID
    return nil, nil
}

func (c *mydownloaderClient) GetFiles(ctx context.Context, id string) ([]File, error) {
    // Get files for a download
    return nil, nil
}

func (c *mydownloaderClient) SSHConfig() config.SSHConfig {
    // Return SSH config for file transfers
    return config.SSHConfig{}
}
```

### 2. Register in server.go

Add the new downloader type to `internal/server/server.go`:

```go
// In the New() function, add a case:
switch dlCfg.Type {
case "qbittorrent":
    // existing code...

case "mydownloader":
    client := download.NewMydownloader(
        name,
        dlCfg,
        download.WithLogger(logger.With().Str("downloader", name).Logger()),
    )
    dlRegistry.Register(name, client)
}
```

### 3. Update Configuration

Add the new type to `internal/config/config.go` if needed.

## Adding a New App

Apps are notified when downloads complete and typically trigger imports.

### 1. Implement the Interface

Add a new file in `internal/app/` with your implementation. Apps implement the package's `configurable`
interface to support shared options (`WithLogger`, `WithCleanupOnCategoryChange`, `WithCleanupOnRemove`):

```go title="internal/app/myapp.go"
package app

import (
    "context"

    "github.com/rs/zerolog"
)

// myappClient is private - only exposed via App interface
type myappClient struct {
    name                    string
    category                string
    downloadsPath           string
    url                     string
    apiKey                  string
    cleanupOnCategoryChange bool
    cleanupOnRemove         bool
    logger                  zerolog.Logger
}

// Implement configurable interface for shared options
func (c *myappClient) setLogger(logger zerolog.Logger) {
    c.logger = logger
}

func (c *myappClient) setCleanupOnCategoryChange(cleanup bool) {
    c.cleanupOnCategoryChange = cleanup
}

func (c *myappClient) setCleanupOnRemove(cleanup bool) {
    c.cleanupOnRemove = cleanup
}

// NewMyapp returns the App interface, not the concrete type
func NewMyapp(name, url, apiKey, category, downloadsPath string, opts ...Option) App {
    c := &myappClient{
        name:          name,
        url:           url,
        apiKey:        apiKey,
        category:      category,
        downloadsPath: downloadsPath,
        logger:        zerolog.Nop(),
    }

    for _, opt := range opts {
        opt(c)
    }

    return c
}

func (c *myappClient) Name() string {
    return c.name
}

func (c *myappClient) Type() string {
    return "myapp"
}

func (c *myappClient) Category() string {
    return c.category
}

func (c *myappClient) DownloadsPath() string {
    return c.downloadsPath
}

func (c *myappClient) CleanupOnCategoryChange() bool {
    return c.cleanupOnCategoryChange
}

func (c *myappClient) CleanupOnRemove() bool {
    return c.cleanupOnRemove
}

func (c *myappClient) TriggerImport(ctx context.Context, path string) error {
    // Call your app's API to trigger an import
    c.logger.Info().
        Str("path", path).
        Msg("triggering import")

    // Make API call...
    return nil
}

func (c *myappClient) TestConnection(ctx context.Context) error {
    // Verify the app is reachable and API key is valid
    return nil
}
```

### 2. Register in server.go

Add the new app type to `internal/server/server.go`:

```go
// In the New() function, add a case:
switch appCfg.Type {
case "sonarr":
    // existing code...

case "myapp":
    client := app.NewMyapp(
        name,
        appCfg.URL,
        appCfg.APIKey,
        appCfg.Category,
        appCfg.DownloadsPath,
        app.WithLogger(logger.With().Str("app", name).Logger()),
        app.WithCleanupOnCategoryChange(appCfg.CleanupOnCategoryChange),
        app.WithCleanupOnRemove(appCfg.CleanupOnRemove),
    )
    appRegistry.Register(name, client)
}
```

## Interface Definitions

### Downloader Interface

```go
type Downloader interface {
    Name() string
    Type() string
    Connect(ctx context.Context) error
    Close() error
    ListDownloads(ctx context.Context, categories []string) ([]Download, error)
    GetDownload(ctx context.Context, id string) (*Download, error)
    GetFiles(ctx context.Context, id string) ([]File, error)
    SSHConfig() config.SSHConfig
}
```

### App Interface

```go
type App interface {
    Name() string
    Type() string
    Category() string
    DownloadsPath() string
    CleanupOnCategoryChange() bool
    CleanupOnRemove() bool
    TriggerImport(ctx context.Context, path string) error
    TestConnection(ctx context.Context) error
}
```

## Testing

Add tests for your new implementations:

```go title="internal/download/mydownloader_test.go"
package download

import (
    "context"
    "testing"
)

func TestMydownloader(t *testing.T) {
    cfg := config.DownloaderConfig{
        URL: "http://localhost:8080",
    }
    client := NewMydownloader("test", cfg)

    if client.Name() != "test" {
        t.Errorf("expected name 'test', got %q", client.Name())
    }

    if client.Type() != "mydownloader" {
        t.Errorf("expected type 'mydownloader', got %q", client.Type())
    }
}
```

Run tests:

```bash
go test ./internal/download/...
```
