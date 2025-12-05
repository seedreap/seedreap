package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/orchestrator"
	testutil "github.com/seedreap/seedreap/internal/testing"
	"github.com/seedreap/seedreap/internal/transfer"
)

// testOrchestrator wraps an orchestrator with test helpers.
type testOrchestrator struct {
	t             *testing.T
	orch          *orchestrator.Orchestrator
	dlRegistry    *download.Registry
	appRegistry   *app.Registry
	syncer        *filesync.Syncer
	mockDL        *testutil.MockDownloader
	mockTransfer  *testutil.MockTransferer
	tmpDir        string
	downloadsPath string
	syncingPath   string
	ctx           context.Context
	cancel        context.CancelFunc
}

// newTestOrchestrator creates a new test orchestrator with mocks.
func newTestOrchestrator(t *testing.T) *testOrchestrator {
	t.Helper()

	tmpDir := t.TempDir()
	downloadsPath := filepath.Join(tmpDir, "downloads")
	syncingPath := filepath.Join(tmpDir, "syncing")

	require.NoError(t, os.MkdirAll(downloadsPath, 0750))
	require.NoError(t, os.MkdirAll(syncingPath, 0750))

	// Create mock downloader
	mockDL := testutil.NewMockDownloader("test-downloader")

	// Create mock transferer
	mockTransfer := testutil.NewMockTransferer()

	// Create registries
	dlRegistry := download.NewRegistry()
	dlRegistry.Register("test-downloader", mockDL)

	appRegistry := app.NewRegistry()

	// Create syncer with mock transferer
	syncr := filesync.New(syncingPath,
		filesync.WithMaxConcurrent(2),
		filesync.WithTransferer(mockTransfer),
	)

	// Create orchestrator with short poll interval for testing
	orch := orchestrator.New(
		dlRegistry,
		appRegistry,
		syncr,
		downloadsPath,
		orchestrator.WithPollInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	return &testOrchestrator{
		t:             t,
		orch:          orch,
		dlRegistry:    dlRegistry,
		appRegistry:   appRegistry,
		syncer:        syncr,
		mockDL:        mockDL,
		mockTransfer:  mockTransfer,
		tmpDir:        tmpDir,
		downloadsPath: downloadsPath,
		syncingPath:   syncingPath,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// addApp adds a mock app to the registry.
func (to *testOrchestrator) addApp(name, category string, opts ...func(*testutil.MockApp)) *testutil.MockApp {
	to.t.Helper()
	appDownloadsPath := filepath.Join(to.downloadsPath, category)
	require.NoError(to.t, os.MkdirAll(appDownloadsPath, 0750))

	mockApp := testutil.NewMockApp(name, category, appDownloadsPath)
	for _, opt := range opts {
		opt(mockApp)
	}
	to.appRegistry.Register(name, mockApp)
	return mockApp
}

// start starts the orchestrator.
func (to *testOrchestrator) start() {
	to.t.Helper()
	require.NoError(to.t, to.orch.Start(to.ctx))
}

// stop stops the orchestrator.
func (to *testOrchestrator) stop() {
	to.t.Helper()
	to.cancel()
	to.orch.Stop()
}

// waitForState waits for a download to reach a specific state.
func (to *testOrchestrator) waitForState(
	downloadID string, state orchestrator.DownloadState, timeout time.Duration,
) bool {
	to.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, td := range to.orch.GetTrackedDownloads() {
			dl := td.GetDownload()
			if dl != nil && dl.ID == downloadID && td.GetState() == state {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForTracked waits for a download to be tracked (with 500ms timeout).
func (to *testOrchestrator) waitForTracked(downloadID string) bool {
	to.t.Helper()
	const timeout = 500 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, td := range to.orch.GetTrackedDownloads() {
			dl := td.GetDownload()
			if dl != nil && dl.ID == downloadID {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForUntracked waits for a download to be untracked (removed from tracking).
func (to *testOrchestrator) waitForUntracked(downloadID string, timeout time.Duration) bool {
	to.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		found := false
		for _, td := range to.orch.GetTrackedDownloads() {
			dl := td.GetDownload()
			if dl != nil && dl.ID == downloadID {
				found = true
				break
			}
		}
		if !found {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// getTrackedDownload returns a tracked download by ID.
func (to *testOrchestrator) getTrackedDownload(downloadID string) *orchestrator.TrackedDownload {
	for _, td := range to.orch.GetTrackedDownloads() {
		dl := td.GetDownload()
		if dl != nil && dl.ID == downloadID {
			return td
		}
	}
	return nil
}

// createTestDownload creates a complete test download with files.
func createTestDownload(id, name, category string) (*download.Download, []download.File) {
	dl := &download.Download{
		ID:          id,
		Name:        name,
		Hash:        id,
		Category:    category,
		State:       download.TorrentStateComplete,
		SavePath:    "/remote/downloads",
		ContentPath: "/remote/downloads/" + name,
		Size:        1024 * 1024, // 1 MB
		Progress:    1.0,
	}

	files := []download.File{
		{
			Path:       name + "/file1.mkv",
			Size:       512 * 1024,
			Downloaded: 512 * 1024,
			State:      download.FileStateComplete,
			Priority:   1,
		},
		{
			Path:       name + "/file2.mkv",
			Size:       512 * 1024,
			Downloaded: 512 * 1024,
			State:      download.FileStateComplete,
			Priority:   1,
		},
	}

	dl.Files = files
	return dl, files
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- Download Removal Tests ---

func TestDownloadRemoved(t *testing.T) {
	t.Run("Complete", func(t *testing.T) {
		tests := []struct {
			name            string
			cleanupOnRemove bool
			expectCleanup   bool
		}{
			{
				name:            "CleanupEnabled",
				cleanupOnRemove: true,
				expectCleanup:   true,
			},
			{
				name:            "CleanupDisabled",
				cleanupOnRemove: false,
				expectCleanup:   false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				to := newTestOrchestrator(t)
				defer to.stop()

				// Add app with configured cleanup setting
				to.addApp("sonarr", "tv-sonarr", func(a *testutil.MockApp) {
					a.SetCleanupOnRemove(tt.cleanupOnRemove)
				})

				// Add a complete download
				dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
				to.mockDL.AddDownload(dl, files)

				// Start orchestrator - let it sync
				to.start()

				// Wait for download to reach complete state
				require.True(t, to.waitForTracked("hash1"), "download should be tracked")
				require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
					"download should reach complete state")

				// Verify files exist before removal
				filePath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name, "file1.mkv")
				assert.True(t, fileExists(filePath), "synced file should exist before removal")

				// Remove download from downloader
				to.mockDL.RemoveDownload("hash1")

				// Wait for download to be untracked
				require.True(t, to.waitForUntracked("hash1", 500*time.Millisecond),
					"download should be untracked after removal")

				// Verify cleanup behavior
				if tt.expectCleanup {
					assert.False(t, fileExists(filePath), "synced files should be cleaned up")
				} else {
					assert.True(t, fileExists(filePath), "synced files should remain")
				}
			})
		}
	})

	t.Run("WhileSyncing", func(t *testing.T) {
		t.Run("ShouldCancelAndCleanup", func(t *testing.T) {
			to := newTestOrchestrator(t)
			defer to.stop()

			// Add app
			to.addApp("sonarr", "tv-sonarr")

			// Create download that will take time to sync
			dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")

			// Make transfer slow so we can catch it mid-sync
			to.mockTransfer.OnTransfer = func(ctx context.Context, req transfer.Request, onProgress transfer.ProgressFunc) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					if onProgress != nil {
						onProgress(transfer.Progress{Transferred: req.Size, BytesPerSec: 1024})
					}
					return nil
				}
			}

			to.mockDL.AddDownload(dl, files)

			// Start orchestrator
			to.start()

			// Wait for download to start syncing
			require.True(t, to.waitForTracked("hash1"), "download should be tracked")
			require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond),
				"download should start syncing")

			// Remove download while syncing
			to.mockDL.RemoveDownload("hash1")

			// Wait for download to be untracked (sync should be cancelled)
			require.True(t, to.waitForUntracked("hash1", 1*time.Second),
				"download should be untracked after removal while syncing")

			// Verify staging files were cleaned up
			stagingPath := filepath.Join(to.syncingPath, "test-downloader", "hash1")
			assert.False(t, fileExists(stagingPath), "staging directory should be cleaned up")
		})
	})

	t.Run("NotTracked", func(t *testing.T) {
		t.Run("NoError", func(t *testing.T) {
			to := newTestOrchestrator(t)
			defer to.stop()

			// Add app
			to.addApp("sonarr", "tv-sonarr")

			// Start orchestrator without adding any downloads
			to.start()

			// Wait a bit for poll to happen
			time.Sleep(100 * time.Millisecond)

			// Verify no tracked downloads
			assert.Empty(t, to.orch.GetTrackedDownloads(), "should have no tracked downloads")
		})
	})

	t.Run("MultipleDownloads", func(t *testing.T) {
		t.Run("OneRemovedOthersUnaffected", func(t *testing.T) {
			to := newTestOrchestrator(t)
			defer to.stop()

			// Add app
			to.addApp("sonarr", "tv-sonarr")

			// Add two downloads
			dl1, files1 := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
			dl2, files2 := createTestDownload("hash2", "TestShow.S01E02", "tv-sonarr")

			to.mockDL.AddDownload(dl1, files1)
			to.mockDL.AddDownload(dl2, files2)

			// Start orchestrator - let it sync
			to.start()

			// Wait for both to reach complete state
			require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second))
			require.True(t, to.waitForState("hash2", orchestrator.StateComplete, 2*time.Second))

			// Remove first download
			to.mockDL.RemoveDownload("hash1")

			// Wait for first to be untracked
			require.True(t, to.waitForUntracked("hash1", 500*time.Millisecond))

			// Verify second is still tracked and complete
			td2 := to.getTrackedDownload("hash2")
			require.NotNil(t, td2, "second download should still be tracked")
			assert.Equal(t, orchestrator.StateComplete, td2.GetState())

			// Remove second download and verify it becomes untracked
			to.mockDL.RemoveDownload("hash2")
			require.True(t, to.waitForUntracked("hash2", 500*time.Millisecond))
		})
	})
}

// --- Category Change Tests ---

// setupCompleteDownload adds a download, starts the orchestrator, and waits for it to complete.
// Returns the file path of the synced file.
func (to *testOrchestrator) setupCompleteDownload(t *testing.T, dlID, dlName, category string) string {
	t.Helper()

	dl, _ := createTestDownload(dlID, dlName, category)
	to.mockDL.AddDownload(dl, dl.Files)

	to.start()

	require.True(t, to.waitForTracked(dlID), "download should be tracked")
	require.True(t, to.waitForState(dlID, orchestrator.StateComplete, 2*time.Second),
		"download should reach complete state")

	return filepath.Join(to.downloadsPath, category, dlName, "file1.mkv")
}

// configureSlowTransfer sets up the mock transferer to simulate slow transfers that can be cancelled.
func (to *testOrchestrator) configureSlowTransfer(duration time.Duration) {
	to.mockTransfer.OnTransfer = func(
		ctx context.Context, req transfer.Request, onProgress transfer.ProgressFunc,
	) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(duration):
			if onProgress != nil {
				onProgress(transfer.Progress{Transferred: req.Size, BytesPerSec: 1024})
			}
			return nil
		}
	}
}

// configureTransferWithFileCreation sets up the mock transferer to create files after a delay.
func (to *testOrchestrator) configureTransferWithFileCreation(delay time.Duration) {
	to.mockTransfer.OnTransfer = func(
		_ context.Context, req transfer.Request, onProgress transfer.ProgressFunc,
	) error {
		time.Sleep(delay)
		if onProgress != nil {
			onProgress(transfer.Progress{Transferred: req.Size, BytesPerSec: 1024 * 1024})
		}

		if err := os.MkdirAll(filepath.Dir(req.LocalPath), 0750); err != nil {
			return err
		}
		return os.WriteFile(req.LocalPath, make([]byte, req.Size), 0644)
	}
}

func TestCategoryChangedToUntracked_Complete(t *testing.T) {
	tests := []struct {
		name                    string
		cleanupOnCategoryChange bool
		expectCleanup           bool
	}{
		{"CleanupEnabled", true, true},
		{"CleanupDisabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to := newTestOrchestrator(t)
			defer to.stop()

			to.addApp("sonarr", "tv-sonarr", func(a *testutil.MockApp) {
				a.SetCleanupOnCategoryChange(tt.cleanupOnCategoryChange)
			})

			filePath := to.setupCompleteDownload(t, "hash1", "TestShow.S01E01", "tv-sonarr")
			assert.True(t, fileExists(filePath), "synced file should exist")

			to.mockDL.SetCategory("hash1", "untracked-category")
			require.True(t, to.waitForUntracked("hash1", 500*time.Millisecond),
				"download should be untracked after category change")

			if tt.expectCleanup {
				assert.False(t, fileExists(filePath), "synced files should be cleaned up")
			} else {
				assert.True(t, fileExists(filePath), "synced files should remain")
			}
		})
	}
}

func TestCategoryChangedToUntracked_WhileSyncing(t *testing.T) {
	to := newTestOrchestrator(t)
	defer to.stop()

	to.addApp("sonarr", "tv-sonarr")
	to.configureSlowTransfer(2 * time.Second)

	dl, _ := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
	to.mockDL.AddDownload(dl, dl.Files)

	to.start()

	require.True(t, to.waitForTracked("hash1"), "download should be tracked")
	require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond),
		"download should start syncing")

	to.mockDL.SetCategory("hash1", "untracked-category")

	require.True(t, to.waitForUntracked("hash1", 1*time.Second),
		"download should be untracked after category change while syncing")

	stagingPath := filepath.Join(to.syncingPath, "test-downloader", "hash1")
	assert.False(t, fileExists(stagingPath), "staging directory should be cleaned up")
}

func TestCategoryChangedToTrackedApp_Complete(t *testing.T) {
	to := newTestOrchestrator(t)
	defer to.stop()

	sonarrApp := to.addApp("sonarr", "tv-sonarr")
	radarrApp := to.addApp("radarr", "movies")

	dl, _ := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
	sonarrFilePath := to.setupCompleteDownload(t, "hash1", dl.Name, "tv-sonarr")
	assert.True(t, fileExists(sonarrFilePath), "synced file should exist in sonarr location")

	to.mockDL.SetCategory("hash1", "movies")

	require.True(t, to.waitForUntracked("hash1", 1*time.Second),
		"download should be untracked after migration")

	// Verify migration
	radarrFilePath := filepath.Join(radarrApp.DownloadsPath(), "test-downloader", dl.Name, "file1.mkv")
	assert.True(t, fileExists(radarrFilePath), "files should be migrated to new app location")
	assert.False(t, fileExists(sonarrFilePath), "files should be removed from old location")
	assert.Len(t, radarrApp.GetImportCalls(), 1, "import should be triggered on new app")

	// Ensure sonarrApp variable is used
	_ = sonarrApp
}

func TestCategoryChangedToTrackedApp_WhileSyncing(t *testing.T) {
	to := newTestOrchestrator(t)
	defer to.stop()

	to.addApp("sonarr", "tv-sonarr")
	radarrApp := to.addApp("radarr", "movies")
	to.configureTransferWithFileCreation(100 * time.Millisecond)

	dl, _ := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
	to.mockDL.AddDownload(dl, dl.Files)

	to.start()

	require.True(t, to.waitForTracked("hash1"), "download should be tracked")
	require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond),
		"download should start syncing")

	to.mockDL.SetCategory("hash1", "movies")

	require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
		"download should complete")

	td := to.getTrackedDownload("hash1")
	require.NotNil(t, td, "download should still be tracked")

	job := td.GetSyncJob()
	require.NotNil(t, job, "sync job should exist")
	assert.Contains(t, job.GetFinalPath(), radarrApp.DownloadsPath(),
		"job should be redirected to new app path")
}

// TestCategoryChangedToTrackedApp_AfterRestart tests that category changes are handled
// correctly when a download was already synced (no SyncJob exists because files already
// existed at destination). This simulates the scenario after a restart where files are
// already present.
func TestCategoryChangedToTrackedApp_AfterRestart(t *testing.T) {
	to := newTestOrchestrator(t)
	defer to.stop()

	to.addApp("sonarr", "tv-sonarr")
	radarrApp := to.addApp("radarr", "movies")

	dl, _ := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")

	// Pre-create the files at the final destination to simulate already-synced state
	// This is what happens after a restart - files exist, so no SyncJob is created
	sonarrFinalPath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name)
	require.NoError(t, os.MkdirAll(sonarrFinalPath, 0750))
	sonarrFilePath := filepath.Join(sonarrFinalPath, "file1.mkv")
	require.NoError(t, os.WriteFile(sonarrFilePath, make([]byte, 512*1024), 0644))
	sonarrFilePath2 := filepath.Join(sonarrFinalPath, "file2.mkv")
	require.NoError(t, os.WriteFile(sonarrFilePath2, make([]byte, 512*1024), 0644))

	// Now add the download to the mock - orchestrator should detect files exist
	to.mockDL.AddDownload(dl, dl.Files)
	to.start()

	require.True(t, to.waitForTracked("hash1"), "download should be tracked")
	require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
		"download should reach complete state (files already exist)")

	// Verify no SyncJob was created (this is the key condition for the bug)
	td := to.getTrackedDownload("hash1")
	require.NotNil(t, td, "download should be tracked")
	assert.Nil(t, td.GetSyncJob(), "sync job should be nil when files already exist")

	// Now change category - this should still migrate the files
	to.mockDL.SetCategory("hash1", "movies")

	require.True(t, to.waitForUntracked("hash1", 1*time.Second),
		"download should be untracked after migration")

	// Verify migration happened
	radarrFilePath := filepath.Join(radarrApp.DownloadsPath(), "test-downloader", dl.Name, "file1.mkv")
	assert.True(t, fileExists(radarrFilePath), "files should be migrated to new app location")
	assert.False(t, fileExists(sonarrFilePath), "files should be removed from old location")
	assert.Len(t, radarrApp.GetImportCalls(), 1, "import should be triggered on new app")
}

// --- Basic Lifecycle Tests ---

func TestBasicLifecycle(t *testing.T) {
	t.Run("DiscoverySyncComplete", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		app := to.addApp("sonarr", "tv-sonarr")
		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()

		// Should progress through states: Discovered -> Syncing -> Synced -> Moving -> Importing -> Complete
		require.True(t, to.waitForTracked("hash1"), "download should be tracked")
		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
			"download should reach complete state")

		// Verify files were synced
		filePath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name, "file1.mkv")
		assert.True(t, fileExists(filePath), "synced file should exist")

		// Verify import was triggered
		assert.Len(t, app.GetImportCalls(), 1, "import should be triggered")
	})

	t.Run("DownloadNotTrackedWithoutMatchingApp", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		// Add app for different category
		to.addApp("sonarr", "tv-sonarr")

		// Add download with non-matching category
		dl, files := createTestDownload("hash1", "Movie.2024", "movies")
		to.mockDL.AddDownload(dl, files)

		to.start()

		// Wait for poll to happen
		time.Sleep(100 * time.Millisecond)

		// Should not be tracked
		assert.Empty(t, to.orch.GetTrackedDownloads(), "download should not be tracked without matching app")
	})

	t.Run("MultipleAppsForSameCategory", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		// Add two apps for the same category
		app1 := to.addApp("sonarr", "tv-sonarr")
		app2 := testutil.NewMockApp("sonarr-4k", "tv-sonarr", filepath.Join(to.downloadsPath, "tv-sonarr-4k"))
		require.NoError(t, os.MkdirAll(app2.DownloadsPath(), 0750))
		to.appRegistry.Register("sonarr-4k", app2)

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
			"download should reach complete state")

		// Both apps should receive import trigger
		assert.Len(t, app1.GetImportCalls(), 1, "first app should receive import")
		assert.Len(t, app2.GetImportCalls(), 1, "second app should receive import")
	})
}

// --- Download Filtering Tests ---

func TestDownloadFiltering(t *testing.T) {
	t.Run("IgnoresDownloadsWithNoMatchingApp", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		// Add app for TV category only
		to.addApp("sonarr", "tv-sonarr")

		// Add download with non-matching category
		dl, files := createTestDownload("hash1", "Movie.2024", "movies")
		to.mockDL.AddDownload(dl, files)

		to.start()

		// Wait for multiple poll cycles
		time.Sleep(150 * time.Millisecond)

		// Should not be tracked
		assert.Empty(t, to.orch.GetTrackedDownloads(), "download should not be tracked without matching app")

		// Verify no transfers were attempted
		assert.Empty(t, to.mockTransfer.GetTransferCalls(), "no transfers should occur for unmatched downloads")
	})

	t.Run("IgnoresDownloadsWithEmptyCategory", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Add download with empty category
		dl, files := createTestDownload("hash1", "Unknown.Download", "")
		to.mockDL.AddDownload(dl, files)

		to.start()

		time.Sleep(150 * time.Millisecond)

		assert.Empty(t, to.orch.GetTrackedDownloads(), "download with empty category should not be tracked")
	})

	t.Run("TracksOnlyMatchingDownloads", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Add both matching and non-matching downloads
		dl1, files1 := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr") // Matches
		dl2, files2 := createTestDownload("hash2", "Movie.2024", "movies")         // No match
		dl3, files3 := createTestDownload("hash3", "TestShow.S01E02", "tv-sonarr") // Matches

		to.mockDL.AddDownload(dl1, files1)
		to.mockDL.AddDownload(dl2, files2)
		to.mockDL.AddDownload(dl3, files3)

		to.start()

		// Wait for both matching downloads to complete
		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second))
		require.True(t, to.waitForState("hash3", orchestrator.StateComplete, 2*time.Second))

		// Verify only 2 downloads are tracked (the matching ones)
		tracked := to.orch.GetTrackedDownloads()
		assert.Len(t, tracked, 2, "only matching downloads should be tracked")

		// Verify the non-matching one is not tracked
		for _, td := range tracked {
			dl := td.GetDownload()
			assert.NotEqual(t, "hash2", dl.ID, "non-matching download should not be tracked")
		}
	})

	t.Run("IgnoresDownloadsWhenNoAppsConfigured", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		// Don't add any apps

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()

		time.Sleep(150 * time.Millisecond)

		assert.Empty(t, to.orch.GetTrackedDownloads(), "no downloads should be tracked when no apps configured")
	})
}

// --- Incremental Sync Tests ---

func TestIncrementalSync(t *testing.T) {
	t.Run("WaitsForCompleteFiles", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Create download with incomplete files
		dl := &download.Download{
			ID:       "hash1",
			Name:     "TestShow.S01E01",
			Hash:     "hash1",
			Category: "tv-sonarr",
			State:    download.TorrentStateDownloading,
			Size:     1024 * 1024,
			Progress: 0.5,
		}
		files := []download.File{
			{
				Path:       dl.Name + "/file1.mkv",
				Size:       512 * 1024,
				Downloaded: 256 * 1024, // Not complete
				State:      download.FileStateDownloading,
				Priority:   1,
			},
		}
		dl.Files = files
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")

		// Wait a bit - should stay in discovered state (no complete files)
		time.Sleep(150 * time.Millisecond)
		td := to.getTrackedDownload("hash1")
		require.NotNil(t, td)
		assert.Equal(t, orchestrator.StateDiscovered, td.GetState(),
			"should stay in discovered state with no complete files")
	})

	t.Run("SyncsAsFilesComplete", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Create download with one complete file and one incomplete
		dl := &download.Download{
			ID:       "hash1",
			Name:     "TestShow.S01E01",
			Hash:     "hash1",
			Category: "tv-sonarr",
			State:    download.TorrentStateDownloading,
			Size:     1024 * 1024,
			Progress: 0.5,
		}
		files := []download.File{
			{
				Path:       dl.Name + "/file1.mkv",
				Size:       512 * 1024,
				Downloaded: 512 * 1024,
				State:      download.FileStateComplete, // Complete
				Priority:   1,
			},
			{
				Path:       dl.Name + "/file2.mkv",
				Size:       512 * 1024,
				Downloaded: 256 * 1024, // Not complete
				State:      download.FileStateDownloading,
				Priority:   1,
			},
		}
		dl.Files = files
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")

		// Should start syncing since there's a complete file
		require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond),
			"should start syncing with one complete file")
	})

	t.Run("SkipsFilesWithPriorityZero", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Create download with only priority 0 files (deselected)
		dl := &download.Download{
			ID:       "hash1",
			Name:     "TestShow.S01E01",
			Hash:     "hash1",
			Category: "tv-sonarr",
			State:    download.TorrentStateComplete,
			Size:     1024 * 1024,
			Progress: 1.0,
		}
		files := []download.File{
			{
				Path:       dl.Name + "/file1.mkv",
				Size:       512 * 1024,
				Downloaded: 512 * 1024,
				State:      download.FileStateComplete,
				Priority:   0, // Deselected
			},
		}
		dl.Files = files
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")

		// Should stay in discovered (no selectable complete files)
		time.Sleep(150 * time.Millisecond)
		td := to.getTrackedDownload("hash1")
		require.NotNil(t, td)
		assert.Equal(t, orchestrator.StateDiscovered, td.GetState(),
			"should stay in discovered with only priority 0 files")
	})
}

// --- Files Already Exist Tests ---

func TestFilesAlreadyExist(t *testing.T) {
	t.Run("SkipsSyncWhenAllFilesExist", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")

		// Pre-create files at final destination
		finalPath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name)
		require.NoError(t, os.MkdirAll(finalPath, 0750))
		for _, f := range files {
			filePath := filepath.Join(to.downloadsPath, "tv-sonarr", f.Path)
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0750))
			require.NoError(t, os.WriteFile(filePath, make([]byte, f.Size), 0644))
		}

		to.mockDL.AddDownload(dl, files)
		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")
		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 500*time.Millisecond),
			"should quickly reach complete state when files exist")

		// Verify no sync job was created
		td := to.getTrackedDownload("hash1")
		require.NotNil(t, td)
		assert.Nil(t, td.GetSyncJob(), "sync job should not be created when files exist")

		// Verify no transfers were attempted
		assert.Empty(t, to.mockTransfer.GetTransferCalls(), "no transfers should occur")
	})

	t.Run("ReSyncsWhenFileSizeMismatch", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")

		// Pre-create files with wrong size
		finalPath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name)
		require.NoError(t, os.MkdirAll(finalPath, 0750))
		for _, f := range files {
			filePath := filepath.Join(to.downloadsPath, "tv-sonarr", f.Path)
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0750))
			// Write file with wrong size (smaller)
			require.NoError(t, os.WriteFile(filePath, make([]byte, f.Size/2), 0644))
		}

		to.mockDL.AddDownload(dl, files)
		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")
		require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond),
			"should start syncing when file sizes don't match")

		// Verify transfers were attempted
		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second))
		assert.NotEmpty(t, to.mockTransfer.GetTransferCalls(), "transfers should occur for mismatched files")
	})
}

// --- Sync Error Tests ---

func TestSyncErrors(t *testing.T) {
	t.Run("TransferError", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Configure transfer to fail
		to.mockTransfer.OnTransfer = func(
			_ context.Context, _ transfer.Request, _ transfer.ProgressFunc,
		) error {
			return errors.New("transfer failed")
		}

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")
		require.True(t, to.waitForState("hash1", orchestrator.StateError, 2*time.Second),
			"should reach error state on transfer failure")

		td := to.getTrackedDownload("hash1")
		require.NotNil(t, td)
		assert.Error(t, td.GetError(), "error should be set")
	})
}

// --- Import Error Tests ---

func TestImportErrors(t *testing.T) {
	t.Run("ContinuesOnImportError", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		app := to.addApp("sonarr", "tv-sonarr")
		app.SetTriggerError(errors.New("import failed"))

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()

		require.True(t, to.waitForTracked("hash1"), "download should be tracked")

		// Should still complete even if import fails
		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second),
			"should reach complete state despite import error")

		// Verify files were synced
		filePath := filepath.Join(to.downloadsPath, "tv-sonarr", dl.Name, "file1.mkv")
		assert.True(t, fileExists(filePath), "synced file should exist")
	})
}

// --- GetStats Tests ---

func TestGetStats(t *testing.T) {
	t.Run("ReturnsCorrectStats", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")

		// Add a complete download
		dl1, files1 := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl1, files1)

		// Add a downloading download
		dl2 := &download.Download{
			ID:       "hash2",
			Name:     "TestShow.S01E02",
			Hash:     "hash2",
			Category: "tv-sonarr",
			State:    download.TorrentStateDownloading,
			Size:     1024 * 1024,
			Progress: 0.5,
		}
		files2 := []download.File{
			{
				Path:       dl2.Name + "/file1.mkv",
				Size:       512 * 1024,
				Downloaded: 512 * 1024,
				State:      download.FileStateComplete,
				Priority:   1,
			},
		}
		dl2.Files = files2
		to.mockDL.AddDownload(dl2, files2)

		to.start()

		require.True(t, to.waitForState("hash1", orchestrator.StateComplete, 2*time.Second))
		require.True(t, to.waitForTracked("hash2"), "second download should be tracked")

		stats := to.orch.GetStats()

		assert.Equal(t, 2, stats["total_tracked"], "should have 2 tracked downloads")
		assert.GreaterOrEqual(t, stats["downloading_on_seedbox"], 1,
			"should have at least 1 downloading on seedbox")

		byState, ok := stats["by_state"].(map[string]int)
		require.True(t, ok, "by_state should be a map")
		assert.Positive(t, byState["complete"], "should have complete downloads")
	})

	t.Run("EmptyWhenNoDownloads", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")
		to.start()

		time.Sleep(100 * time.Millisecond)

		stats := to.orch.GetStats()
		assert.Equal(t, 0, stats["total_tracked"], "should have 0 tracked downloads")
	})
}

// --- Start/Stop Tests ---

func TestStartStop(t *testing.T) {
	t.Run("FailsOnDownloaderConnectionError", func(t *testing.T) {
		to := newTestOrchestrator(t)

		// Configure downloader to fail on connect
		to.mockDL.OnConnect = func(_ context.Context) error {
			return errors.New("connection failed")
		}

		err := to.orch.Start(to.ctx)
		require.Error(t, err, "start should fail when downloader connection fails")
		assert.Contains(t, err.Error(), "connection failed")
	})

	t.Run("ContinuesOnAppConnectionWarning", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		// Add app that fails connection test
		app := to.addApp("sonarr", "tv-sonarr")
		app.TestConnErr = errors.New("app unreachable")

		// Start should succeed (app connection is just a warning)
		err := to.orch.Start(to.ctx)
		assert.NoError(t, err, "start should succeed despite app connection warning")
	})

	t.Run("GracefulShutdown", func(t *testing.T) {
		to := newTestOrchestrator(t)

		to.addApp("sonarr", "tv-sonarr")
		to.configureSlowTransfer(500 * time.Millisecond)

		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()
		require.True(t, to.waitForState("hash1", orchestrator.StateSyncing, 500*time.Millisecond))

		// Stop should complete without hanging
		done := make(chan struct{})
		go func() {
			to.stop()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(15 * time.Second):
			t.Fatal("stop took too long")
		}
	})
}

// --- TrackedDownload Thread Safety Tests ---

func TestTrackedDownloadThreadSafety(t *testing.T) {
	t.Run("ConcurrentAccess", func(t *testing.T) {
		to := newTestOrchestrator(t)
		defer to.stop()

		to.addApp("sonarr", "tv-sonarr")
		dl, files := createTestDownload("hash1", "TestShow.S01E01", "tv-sonarr")
		to.mockDL.AddDownload(dl, files)

		to.start()
		require.True(t, to.waitForTracked("hash1"))

		// Concurrent access to TrackedDownload methods should not race
		done := make(chan struct{})
		for range 10 {
			go func() {
				for range 100 {
					td := to.getTrackedDownload("hash1")
					if td != nil {
						_ = td.GetState()
						_ = td.GetDownload()
						_ = td.GetError()
						_, _ = td.GetTimes()
						_ = td.GetSyncJob()
					}
				}
				done <- struct{}{}
			}()
		}

		// Wait for all goroutines
		for range 10 {
			<-done
		}
	})
}
