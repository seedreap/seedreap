// Package app provides interfaces and implementations for applications that process downloads.
package app

import (
	"context"

	"github.com/rs/zerolog"
)

// configurable is implemented by all apps to support shared options.
type configurable interface {
	setLogger(zerolog.Logger)
	setCleanupOnCategoryChange(bool)
	setCleanupOnRemove(bool)
}

// Option is a functional option for configuring apps.
type Option func(configurable)

// WithLogger sets the logger for any app.
func WithLogger(logger zerolog.Logger) Option {
	return func(c configurable) {
		c.setLogger(logger)
	}
}

// WithCleanupOnCategoryChange sets whether to cleanup files on category change.
func WithCleanupOnCategoryChange(cleanup bool) Option {
	return func(c configurable) {
		c.setCleanupOnCategoryChange(cleanup)
	}
}

// WithCleanupOnRemove sets whether to cleanup files when removed from downloader.
func WithCleanupOnRemove(cleanup bool) Option {
	return func(c configurable) {
		c.setCleanupOnRemove(cleanup)
	}
}

// App is the interface that applications (Sonarr, Radarr, passthrough, etc.) must implement.
type App interface {
	// Name returns the configured name of this app instance.
	Name() string

	// Type returns the type of app (e.g., "sonarr", "radarr", "passthrough").
	Type() string

	// Category returns the download category this app handles.
	Category() string

	// DownloadsPath returns the path where completed downloads should be placed.
	DownloadsPath() string

	// CleanupOnCategoryChange returns true if synced files should be deleted when the
	// download's category changes in the downloader (e.g., post-import category change).
	CleanupOnCategoryChange() bool

	// CleanupOnRemove returns true if synced files should be deleted when the download
	// is removed from the downloader.
	CleanupOnRemove() bool

	// TriggerImport tells the app to scan for and import completed downloads.
	// If path is specified, it scans only that path.
	TriggerImport(ctx context.Context, path string) error

	// TestConnection tests the connection to the app.
	TestConnection(ctx context.Context) error
}

// Registry holds all configured apps.
type Registry struct {
	apps         map[string]App
	byCategory   map[string][]App
	byDownloader map[string][]App
}

// NewRegistry creates a new app registry.
func NewRegistry() *Registry {
	return &Registry{
		apps:         make(map[string]App),
		byCategory:   make(map[string][]App),
		byDownloader: make(map[string][]App),
	}
}

// Register adds an app to the registry.
func (r *Registry) Register(name string, a App) {
	r.apps[name] = a
	r.byCategory[a.Category()] = append(r.byCategory[a.Category()], a)
}

// RegisterForDownloader associates an app with a downloader.
func (r *Registry) RegisterForDownloader(downloaderName string, a App) {
	r.byDownloader[downloaderName] = append(r.byDownloader[downloaderName], a)
}

// Get returns an app by name.
func (r *Registry) Get(name string) (App, bool) {
	a, ok := r.apps[name]
	return a, ok
}

// GetByCategory returns all apps that handle the given category.
func (r *Registry) GetByCategory(category string) []App {
	return r.byCategory[category]
}

// GetByDownloader returns all apps associated with a downloader.
func (r *Registry) GetByDownloader(downloaderName string) []App {
	return r.byDownloader[downloaderName]
}

// All returns all registered apps.
func (r *Registry) All() map[string]App {
	return r.apps
}

// Categories returns all unique categories.
func (r *Registry) Categories() []string {
	cats := make([]string, 0, len(r.byCategory))
	for cat := range r.byCategory {
		cats = append(cats, cat)
	}
	return cats
}
