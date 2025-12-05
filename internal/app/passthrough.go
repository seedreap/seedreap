package app

import (
	"context"

	"github.com/rs/zerolog"
)

// passthroughClient implements the App interface for passthrough downloads.
// It syncs files but does not trigger any API calls on completion.
// It is private and only exposed via the App interface.
type passthroughClient struct {
	name                    string
	category                string
	downloadsPath           string
	cleanupOnCategoryChange bool
	cleanupOnRemove         bool
	logger                  zerolog.Logger
}

// setLogger implements configurable for shared options.
func (c *passthroughClient) setLogger(logger zerolog.Logger) {
	c.logger = logger
}

// setCleanupOnCategoryChange implements configurable for shared options.
func (c *passthroughClient) setCleanupOnCategoryChange(cleanup bool) {
	c.cleanupOnCategoryChange = cleanup
}

// setCleanupOnRemove implements configurable for shared options.
func (c *passthroughClient) setCleanupOnRemove(cleanup bool) {
	c.cleanupOnRemove = cleanup
}

// NewPassthrough creates a new passthrough client and returns it as App.
func NewPassthrough(name, category, downloadsPath string, opts ...Option) App {
	c := &passthroughClient{
		name:          name,
		category:      category,
		downloadsPath: downloadsPath,
		logger:        zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Name returns the configured name of this app instance.
func (c *passthroughClient) Name() string {
	return c.name
}

// Type returns the type of app.
func (c *passthroughClient) Type() string {
	return "passthrough"
}

// Category returns the download category this app handles.
func (c *passthroughClient) Category() string {
	return c.category
}

// DownloadsPath returns the path where completed downloads should be placed.
func (c *passthroughClient) DownloadsPath() string {
	return c.downloadsPath
}

// CleanupOnCategoryChange returns true if synced files should be deleted when
// the download's category changes in the downloader.
func (c *passthroughClient) CleanupOnCategoryChange() bool {
	return c.cleanupOnCategoryChange
}

// CleanupOnRemove returns true if synced files should be deleted when the
// download is removed from the downloader.
func (c *passthroughClient) CleanupOnRemove() bool {
	return c.cleanupOnRemove
}

// TriggerImport is a no-op for passthrough. Files are synced but no import is triggered.
func (c *passthroughClient) TriggerImport(_ context.Context, path string) error {
	c.logger.Debug().
		Str("name", c.name).
		Str("path", path).
		Msg("passthrough complete - no import triggered")
	return nil
}

// TestConnection always succeeds for passthrough since there's nothing to connect to.
func (c *passthroughClient) TestConnection(_ context.Context) error {
	c.logger.Debug().
		Str("name", c.name).
		Msg("passthrough connection test - always succeeds")
	return nil
}
