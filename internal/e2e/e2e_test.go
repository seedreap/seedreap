//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/e2e"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	testutil "github.com/seedreap/seedreap/internal/testing"
)

// TestE2E_HappyPath_BasicWorkflow tests the complete download workflow:
// 1. Download is discovered in qBittorrent (already complete).
// 2. Files are synced from seedbox via SSH/SFTP.
// 3. Files are moved to final downloads location.
// 4. Radarr is notified to import the files.
func TestE2E_HappyPath_BasicWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create test harness
	cfg := e2e.DefaultConfig()
	cfg.Logger = zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)
	cfg.PollInterval = 500 * time.Millisecond // Fast polling for tests

	h := e2e.NewHarness(t, cfg)
	h.Start(ctx, cfg)
	defer h.Stop()

	// Test file details
	const (
		torrentHash = "abc123def456"
		torrentName = "Movie.2024.1080p.BluRay"
		fileName    = "movie.mkv"
		fileSize    = 1 * 1024 * 1024 // 1 MB
	)

	// 1. Create test file on SSH container (simulates file on seedbox)
	remotePath := filepath.Join(torrentName, fileName)
	h.CreateTestFileOnSSH(remotePath, fileSize)

	// 2. Add completed torrent to mock qBittorrent
	h.QBittorrent.AddTorrent(&testutil.FakeTorrent{
		Hash:        torrentHash,
		Name:        torrentName,
		Category:    "radarr",
		State:       "uploading", // Complete and seeding
		Progress:    1.0,
		Size:        fileSize,
		Downloaded:  fileSize,
		SavePath:    h.SSH.RemoteDir,
		ContentPath: filepath.Join(h.SSH.RemoteDir, torrentName),
		AddedOn:     time.Now().Unix(),
		CompletedOn: time.Now().Unix(),
	}, []testutil.FakeFile{
		{Index: 0, Name: filepath.Join(torrentName, fileName), Size: fileSize, Progress: 1.0, Priority: 1},
	})

	// 3. Wait for Radarr import to be triggered (workflow completes fast)
	t.Log("Waiting for complete workflow (discovery -> sync -> move -> import)...")
	cmd, err := h.Radarr.WaitForCommand("DownloadedMoviesScan", 2*time.Minute)
	require.NoError(t, err, "Radarr should receive import command")
	assert.Contains(t, cmd.Path, torrentName, "import path should contain torrent name")
	t.Logf("Radarr import triggered: %s", cmd.Path)

	// 4. Wait for import complete state
	t.Log("Waiting for import complete state...")
	td := h.WaitForTrackedDownload(torrentHash, trackeddownload.StateImported, 30*time.Second)
	require.NotNil(t, td, "tracked download should reach imported state")
	t.Logf("Workflow complete (state: %s)", td.State)

	// 5. Verify file exists in final downloads path
	finalPath := filepath.Join(h.DownloadsPath, "radarr", torrentName, fileName)
	_, err = os.Stat(finalPath)
	require.NoError(t, err, "file should exist in final downloads path: %s", finalPath)
	t.Logf("File exists at: %s", finalPath)

	// 6. Verify events were recorded
	events := h.GetAllEvents()
	t.Logf("Total events recorded: %d", len(events))
	for _, ev := range events {
		t.Logf("  Event: %s (%s)", ev.Type, ev.SubjectType)
	}

	// Check key events are present
	eventTypes := e2e.EventTypes(events)
	assert.Contains(t, eventTypes, "download.discovered", "should have discovery event")
	assert.Contains(t, eventTypes, "sync.started", "should have sync started event")
	assert.Contains(t, eventTypes, "sync.complete", "should have sync complete event")
}

// TestE2E_HappyPath_CategoryChangeCleanup tests that files are cleaned up
// when the download category changes in qBittorrent.
func TestE2E_HappyPath_CategoryChangeCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create test harness
	cfg := e2e.DefaultConfig()
	cfg.Logger = zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)
	cfg.PollInterval = 500 * time.Millisecond

	h := e2e.NewHarness(t, cfg)
	h.Start(ctx, cfg)
	defer h.Stop()

	// Test file details
	const (
		torrentHash = "cleanup123"
		torrentName = "Cleanup.Test.Movie"
		fileName    = "test.mkv"
		fileSize    = 512 * 1024 // 512 KB
	)

	// Create test file and torrent
	remotePath := filepath.Join(torrentName, fileName)
	h.CreateTestFileOnSSH(remotePath, fileSize)

	h.QBittorrent.AddTorrent(&testutil.FakeTorrent{
		Hash:        torrentHash,
		Name:        torrentName,
		Category:    "radarr",
		State:       "uploading",
		Progress:    1.0,
		Size:        fileSize,
		Downloaded:  fileSize,
		SavePath:    h.SSH.RemoteDir,
		ContentPath: filepath.Join(h.SSH.RemoteDir, torrentName),
		AddedOn:     time.Now().Unix(),
		CompletedOn: time.Now().Unix(),
	}, []testutil.FakeFile{
		{Index: 0, Name: filepath.Join(torrentName, fileName), Size: fileSize, Progress: 1.0, Priority: 1},
	})

	// Wait for full workflow to complete
	t.Log("Waiting for import to complete...")
	h.WaitForTrackedDownload(torrentHash, trackeddownload.StateImported, 2*time.Minute)

	// Verify file exists
	finalPath := filepath.Join(h.DownloadsPath, "radarr", torrentName, fileName)
	_, err := os.Stat(finalPath)
	require.NoError(t, err, "file should exist before category change")
	t.Log("File exists, now changing category...")

	// Change category in qBittorrent (simulates post-import category change)
	h.QBittorrent.SetTorrentCategory(torrentHash, "imported")

	// Wait for cleanup event
	t.Log("Waiting for category change to be detected...")
	h.WaitForEvent("category.changed", 30*time.Second)

	// Give cleanup time to happen
	time.Sleep(2 * time.Second)

	// Verify file was deleted (CleanupOnCategoryChange=true)
	_, err = os.Stat(finalPath)
	assert.True(t, os.IsNotExist(err), "file should be deleted after category change")
	t.Log("File cleaned up successfully after category change")

	newPath := filepath.Join(h.DownloadsPath, "imported", torrentName, fileName)
	_, err = os.Stat(newPath)
	assert.True(t, os.IsNotExist(err), "file should not exist in new category path")

}

// TestE2E_HappyPath_DownloadRemovedCleanup tests that files are cleaned up
// when the download is removed from qBittorrent.
func TestE2E_HappyPath_DownloadRemovedCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create test harness
	cfg := e2e.DefaultConfig()
	cfg.Logger = zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)
	cfg.PollInterval = 500 * time.Millisecond

	h := e2e.NewHarness(t, cfg)
	h.Start(ctx, cfg)
	defer h.Stop()

	// Test file details
	const (
		torrentHash = "remove456"
		torrentName = "Remove.Test.Movie"
		fileName    = "test.mkv"
		fileSize    = 512 * 1024 // 512 KB
	)

	// Create test file and torrent
	remotePath := filepath.Join(torrentName, fileName)
	h.CreateTestFileOnSSH(remotePath, fileSize)

	h.QBittorrent.AddTorrent(&testutil.FakeTorrent{
		Hash:        torrentHash,
		Name:        torrentName,
		Category:    "radarr",
		State:       "uploading",
		Progress:    1.0,
		Size:        fileSize,
		Downloaded:  fileSize,
		SavePath:    h.SSH.RemoteDir,
		ContentPath: filepath.Join(h.SSH.RemoteDir, torrentName),
		AddedOn:     time.Now().Unix(),
		CompletedOn: time.Now().Unix(),
	}, []testutil.FakeFile{
		{Index: 0, Name: filepath.Join(torrentName, fileName), Size: fileSize, Progress: 1.0, Priority: 1},
	})

	// Wait for full workflow to complete
	t.Log("Waiting for import to complete...")
	h.WaitForTrackedDownload(torrentHash, trackeddownload.StateImported, 2*time.Minute)

	// Verify file exists
	finalPath := filepath.Join(h.DownloadsPath, "radarr", torrentName, fileName)
	_, err := os.Stat(finalPath)
	require.NoError(t, err, "file should exist before removal")
	t.Log("File exists, now removing torrent...")

	// Remove torrent from qBittorrent
	h.QBittorrent.RemoveTorrent(torrentHash)

	// Wait for removal event
	t.Log("Waiting for removal to be detected...")
	h.WaitForEvent("download.removed", 30*time.Second)

	// Give cleanup time to happen
	time.Sleep(2 * time.Second)

	// Verify file was deleted (CleanupOnRemove=true)
	_, err = os.Stat(finalPath)
	assert.True(t, os.IsNotExist(err), "file should be deleted after torrent removal")
	t.Log("File cleaned up successfully after torrent removal")
}

// TestE2E_InProgressDownload tests that downloads still in progress
// are discovered but sync doesn't start until complete.
func TestE2E_InProgressDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Create test harness
	cfg := e2e.DefaultConfig()
	cfg.Logger = zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.DebugLevel)
	cfg.PollInterval = 500 * time.Millisecond

	h := e2e.NewHarness(t, cfg)
	h.Start(ctx, cfg)
	defer h.Stop()

	// Test file details
	const (
		torrentHash = "inprogress789"
		torrentName = "InProgress.Test.Movie"
		fileName    = "test.mkv"
		fileSize    = 1 * 1024 * 1024 // 1 MB
	)

	// Create test file (will be used when download completes)
	remotePath := filepath.Join(torrentName, fileName)
	h.CreateTestFileOnSSH(remotePath, fileSize)

	// Add in-progress torrent (50% complete)
	h.QBittorrent.AddTorrent(&testutil.FakeTorrent{
		Hash:        torrentHash,
		Name:        torrentName,
		Category:    "radarr",
		State:       "downloading",
		Progress:    0.5,
		Size:        fileSize,
		Downloaded:  fileSize / 2,
		SavePath:    h.SSH.RemoteDir,
		ContentPath: filepath.Join(h.SSH.RemoteDir, torrentName),
		AddedOn:     time.Now().Unix(),
	}, []testutil.FakeFile{
		{Index: 0, Name: filepath.Join(torrentName, fileName), Size: fileSize, Progress: 0.5, Priority: 1},
	})

	// Wait for discovery
	t.Log("Waiting for in-progress download to be discovered...")
	td := h.WaitForTrackedDownload(torrentHash, trackeddownload.StateDownloading, 30*time.Second)
	require.NotNil(t, td)
	t.Logf("Download discovered in downloading state: %s", td.State)

	// Verify it doesn't immediately start syncing
	time.Sleep(2 * time.Second)
	tds, _ := h.DB.TrackedDownload.Query().WithDownloadJob().All(ctx)
	for _, td := range tds {
		if dj := td.Edges.DownloadJob; dj != nil && dj.RemoteID == torrentHash {
			assert.Equal(t, trackeddownload.StateDownloading, td.State,
				"should stay in downloading state while incomplete")
		}
	}

	// Complete the download
	t.Log("Completing download...")
	h.QBittorrent.SetTorrentState(torrentHash, "uploading", 1.0)

	// Now wait for sync to start and complete
	t.Log("Waiting for sync after download completes...")
	h.WaitForTrackedDownload(torrentHash, trackeddownload.StateImported, 2*time.Minute)
	t.Log("Download completed and imported successfully")
}
