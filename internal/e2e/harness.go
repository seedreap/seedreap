//go:build e2e

// Package e2e provides end-to-end testing infrastructure.
package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/event"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	"github.com/seedreap/seedreap/internal/server"
	testutil "github.com/seedreap/seedreap/internal/testing"
)

// Test configuration constants.
const (
	sshReadyTimeout       = 30 * time.Second
	serverShutdownTimeout = 10 * time.Second
	containerCleanup      = 30 * time.Second
	defaultHTTPTimeout    = 10 * time.Second
	defaultSSHTimeout     = 10 * time.Second
	pollSleepInterval     = 100 * time.Millisecond
	maxConcurrentSyncs    = 2
	parallelConnections   = 4
)

// Harness provides a complete test environment for end-to-end tests.
// It manages mock servers, containers, and the application server.
type Harness struct {
	t *testing.T

	// Mock servers
	QBittorrent *testutil.QBittorrentServer
	Radarr      *testutil.ArrServer
	Sonarr      *testutil.ArrServer

	// SSH container for file transfers
	SSH *testutil.SSHContainer

	// Application server
	Server *server.Server

	// Database client (shortcut to Server.DB())
	DB *generated.Client

	// File paths
	TempDir       string
	DownloadsPath string
	SyncingPath   string

	// Internal
	ctx       context.Context
	ctxCancel context.CancelFunc
	logger    zerolog.Logger
}

// Config configures the E2E test harness.
type Config struct {
	// PollInterval is how often the download controller polls for changes.
	// Shorter = faster tests, default = 1s for tests
	PollInterval time.Duration

	// EnableSonarr creates a mock Sonarr server
	EnableSonarr bool

	// EnableRadarr creates a mock Radarr server
	EnableRadarr bool

	// RadarrCategory is the category for Radarr (default: "radarr")
	RadarrCategory string

	// SonarrCategory is the category for Sonarr (default: "sonarr")
	SonarrCategory string

	// Logger for the test harness
	Logger zerolog.Logger
}

// DefaultConfig returns sensible defaults for E2E tests.
func DefaultConfig() Config {
	return Config{
		PollInterval:   1 * time.Second,
		EnableRadarr:   true,
		RadarrCategory: "radarr",
		Logger:         zerolog.Nop(),
	}
}

// NewHarness creates a new E2E test harness.
// Call Start() to initialize all components.
func NewHarness(t *testing.T, cfg Config) *Harness {
	t.Helper()

	return &Harness{
		t:      t,
		logger: cfg.Logger,
	}
}

// Start initializes all components of the test harness.
// This starts containers, mock servers, and the application server.
func (h *Harness) Start(ctx context.Context, cfg Config) {
	h.t.Helper()

	// Create cancellable context for cleanup
	h.ctx, h.ctxCancel = context.WithCancel(ctx)

	// Create temp directories
	h.TempDir = h.t.TempDir()
	h.DownloadsPath = filepath.Join(h.TempDir, "downloads")
	h.SyncingPath = filepath.Join(h.TempDir, "syncing")

	// Start SSH container for file transfers
	sshCfg := testutil.DefaultSSHContainerConfig()
	var err error
	h.SSH, err = testutil.StartSSHContainer(h.ctx, sshCfg)
	require.NoError(h.t, err, "failed to start SSH container")

	// Wait for SSH to be ready
	err = h.SSH.WaitForSSH(h.ctx, sshReadyTimeout)
	require.NoError(h.t, err, "SSH container not ready")

	// Start mock qBittorrent server
	h.QBittorrent = testutil.NewQBittorrentServer()

	// Start mock Arr servers
	if cfg.EnableRadarr {
		h.Radarr = testutil.NewArrServer("Radarr")
	}
	if cfg.EnableSonarr {
		h.Sonarr = testutil.NewArrServer("Sonarr")
	}

	// Build application config
	appCfg := h.buildConfig(cfg)

	// Start application server
	h.Server, err = server.New(appCfg, server.Options{
		Logger: cfg.Logger,
	})
	require.NoError(h.t, err, "failed to create server")

	h.DB = h.Server.DB()

	// Start server in background
	go func() {
		_ = h.Server.Run(h.ctx)
	}()

	// Give server time to initialize
	time.Sleep(pollSleepInterval)
}

// buildConfig creates the application config for the test.
func (h *Harness) buildConfig(cfg Config) config.Config {
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 1 * time.Second
	}

	radarrCategory := cfg.RadarrCategory
	if radarrCategory == "" {
		radarrCategory = "radarr"
	}

	sonarrCategory := cfg.SonarrCategory
	if sonarrCategory == "" {
		sonarrCategory = "sonarr"
	}

	appCfg := config.Config{
		Server: config.ServerConfig{
			Listen: "127.0.0.1:0", // Random port
		},
		Database: config.DatabaseConfig{
			DSN: "file::memory:?cache=shared",
		},
		Sync: config.SyncConfig{
			DownloadsPath:       h.DownloadsPath,
			SyncingPath:         h.SyncingPath,
			MaxConcurrent:       maxConcurrentSyncs,
			PollInterval:        pollInterval,
			ParallelConnections: parallelConnections,
		},
		Downloaders: map[string]config.DownloaderConfig{
			"seedbox": {
				Type:        "qbittorrent",
				URL:         h.QBittorrent.URL,
				HTTPTimeout: defaultHTTPTimeout,
				SSH: config.SSHConfig{
					Host:          h.SSH.Host,
					Port:          h.SSH.Port,
					User:          h.SSH.User,
					KeyFile:       h.SSH.PrivateKey,
					IgnoreHostKey: true,
					Timeout:       defaultSSHTimeout,
				},
			},
		},
		Apps: make(map[string]config.AppEntryConfig),
	}

	// Add Radarr app if enabled
	if cfg.EnableRadarr && h.Radarr != nil {
		appCfg.Apps["radarr"] = config.AppEntryConfig{
			Type:                    "radarr",
			URL:                     h.Radarr.URL,
			APIKey:                  "test-api-key",
			Category:                radarrCategory,
			DownloadsPath:           filepath.Join(h.DownloadsPath, radarrCategory),
			HTTPTimeout:             defaultHTTPTimeout,
			CleanupOnCategoryChange: true,
			CleanupOnRemove:         true,
		}
	}

	// Add Sonarr app if enabled
	if cfg.EnableSonarr && h.Sonarr != nil {
		appCfg.Apps["sonarr"] = config.AppEntryConfig{
			Type:                    "sonarr",
			URL:                     h.Sonarr.URL,
			APIKey:                  "test-api-key",
			Category:                sonarrCategory,
			DownloadsPath:           filepath.Join(h.DownloadsPath, sonarrCategory),
			HTTPTimeout:             defaultHTTPTimeout,
			CleanupOnCategoryChange: true,
			CleanupOnRemove:         true,
		}
	}

	return appCfg
}

// Stop shuts down all components.
func (h *Harness) Stop() {
	h.t.Helper()

	// Cancel context to trigger shutdown
	if h.ctxCancel != nil {
		h.ctxCancel()
	}

	// Shutdown server gracefully
	if h.Server != nil {
		h.Server.PrepareShutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		_ = h.Server.Shutdown(shutdownCtx)
	}

	// Close mock servers
	if h.QBittorrent != nil {
		h.QBittorrent.Close()
	}
	if h.Radarr != nil {
		h.Radarr.Close()
	}
	if h.Sonarr != nil {
		h.Sonarr.Close()
	}

	// Cleanup SSH container
	if h.SSH != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), containerCleanup)
		defer cancel()
		_ = h.SSH.Cleanup(cleanupCtx)
	}
}

// CreateTestFileOnSSH creates a test file on the SSH container.
// The path is relative to the SSH container's remote directory.
func (h *Harness) CreateTestFileOnSSH(relativePath string, sizeBytes int64) {
	h.t.Helper()

	err := h.SSH.CreateTestFileWithSize(h.ctx, relativePath, sizeBytes)
	require.NoError(h.t, err, "failed to create test file on SSH")
}

// WaitForTrackedDownload waits for a tracked download to reach the specified state.
// The remoteID is the torrent hash/ID from the download client.
func (h *Harness) WaitForTrackedDownload(
	remoteID string,
	state trackeddownload.State,
	timeout time.Duration,
) *generated.TrackedDownload {
	h.t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Query all and filter
		tds, _ := h.DB.TrackedDownload.Query().
			WithDownloadJob().
			All(h.ctx)

		for _, td := range tds {
			if dj := td.Edges.DownloadJob; dj != nil && dj.RemoteID == remoteID {
				if td.State == state {
					return td
				}
				h.logger.Debug().
					Str("remote_id", remoteID).
					Str("current_state", string(td.State)).
					Str("target_state", string(state)).
					Msg("waiting for state transition")
			}
		}

		time.Sleep(pollSleepInterval)
	}

	h.t.Fatalf("timeout waiting for tracked download %s to reach state %s", remoteID, state)
	return nil
}

// WaitForEvent waits for an event of the specified type to be recorded.
func (h *Harness) WaitForEvent(eventType string, timeout time.Duration) *generated.Event {
	h.t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		events, err := h.DB.Event.Query().
			Where(event.TypeEQ(eventType)).
			Order(event.ByTimestamp()).
			All(h.ctx)

		if err == nil && len(events) > 0 {
			return events[len(events)-1] // Return most recent
		}

		time.Sleep(pollSleepInterval)
	}

	h.t.Fatalf("timeout waiting for event type %s", eventType)
	return nil
}

// GetEventsForDownload returns all events associated with a tracked download.
func (h *Harness) GetEventsForDownload(trackedDownloadID string) []*generated.Event {
	h.t.Helper()

	events, err := h.DB.Event.Query().
		Where(
			event.SubjectTypeEQ(event.SubjectTypeDownload),
			event.SubjectIDEQ(trackedDownloadID),
		).
		Order(event.ByTimestamp()).
		All(h.ctx)

	require.NoError(h.t, err, "failed to query events")
	return events
}

// GetAllEvents returns all events in the database.
func (h *Harness) GetAllEvents() []*generated.Event {
	h.t.Helper()

	events, err := h.DB.Event.Query().
		Order(event.ByTimestamp()).
		All(h.ctx)

	require.NoError(h.t, err, "failed to query events")
	return events
}

// AssertEventTypes checks that the events contain all expected types in order.
func (h *Harness) AssertEventTypes(events []*generated.Event, expectedTypes ...string) {
	h.t.Helper()

	actualTypes := make([]string, len(events))
	for i, e := range events {
		actualTypes[i] = e.Type
	}

	// Check that all expected types appear in order (not necessarily contiguous)
	expectedIdx := 0
	for _, actualType := range actualTypes {
		if expectedIdx < len(expectedTypes) && actualType == expectedTypes[expectedIdx] {
			expectedIdx++
		}
	}

	if expectedIdx < len(expectedTypes) {
		h.t.Errorf("expected event types %v but got %v (missing %v)",
			expectedTypes, actualTypes, expectedTypes[expectedIdx:])
	}
}

// EventTypes extracts event types from a slice of events.
func EventTypes(events []*generated.Event) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}
