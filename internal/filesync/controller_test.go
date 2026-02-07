package filesync_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/syncfile"
	"github.com/seedreap/seedreap/internal/events"
	"github.com/seedreap/seedreap/internal/filesync"
	internaltesting "github.com/seedreap/seedreap/internal/testing"
)

func TestFilesyncController_DownloadDiscovered(t *testing.T) {
	t.Run("does not create sync job when no app for category", func(t *testing.T) {
		// The filesync controller should NOT create sync jobs for downloads
		// that don't have an enabled app for their category. This prevents
		// unnecessary syncing of files that no app will process.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		unmatchedCategory := gofakeit.Noun() + "_unmatched"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with a category that has NO matching app
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(unmatchedCategory).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// NO app is created for unmatchedCategory

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish DownloadDiscovered event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify NO sync job was created for this download
		syncJobs, err := db.SyncJob.Query().All(ctx)
		require.NoError(t, err)
		assert.Empty(t, syncJobs,
			"sync job should NOT be created when no app exists for the download's category")
	})

	t.Run("creates sync job when app exists for category", func(t *testing.T) {
		// The filesync controller SHOULD create sync jobs for downloads
		// that have an enabled app for their category.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		category := gofakeit.Noun()

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the category
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetDownloadsPath(t.TempDir()).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with the matching category
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish DownloadDiscovered event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify sync job WAS created for this download
		syncJobs, err := db.SyncJob.Query().All(ctx)
		require.NoError(t, err)
		assert.Len(t, syncJobs, 1,
			"sync job should be created when an app exists for the download's category")
	})

	t.Run("does not create sync job when app is disabled", func(t *testing.T) {
		// The filesync controller should NOT create sync jobs for downloads
		// when the app for their category is disabled.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		category := gofakeit.Noun()

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a DISABLED app for the category
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetDownloadsPath(t.TempDir()).
			SetEnabled(false). // Disabled!
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with the matching category
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish DownloadDiscovered event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify NO sync job was created for this download
		syncJobs, err := db.SyncJob.Query().All(ctx)
		require.NoError(t, err)
		assert.Empty(t, syncJobs,
			"sync job should NOT be created when the app for the category is disabled")
	})
}

func TestFilesyncController_FileCompleted(t *testing.T) {
	t.Run("does not error when no sync job exists for download", func(t *testing.T) {
		// When FileCompleted events are received for downloads that don't have
		// sync jobs (because there's no enabled app for their category), the
		// controller should silently skip processing without logging errors.
		// This is expected behavior since we intentionally don't create sync
		// jobs for downloads without enabled apps.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		unmatchedCategory := gofakeit.Noun() + "_unmatched"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with a category that has NO matching app
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(unmatchedCategory).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// NO sync job is created for this download (as expected when no app exists)

		// Track errors by using a custom logger that captures log output
		var logBuffer bytes.Buffer
		logger := zerolog.New(&logBuffer).Level(zerolog.ErrorLevel)

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(logger),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish FileCompleted event for a download without a sync job
		bus.Publish(events.Event{
			Type:    events.FileCompleted,
			Subject: dl,
			Data: map[string]any{
				"file_path":        "episode.mkv",
				"file_size":        int64(1000000),
				"download_file_id": gofakeit.UUID(),
			},
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify NO error was logged - this should be debug level at most
		// since it's expected behavior for downloads without enabled apps
		logOutput := logBuffer.String()
		assert.Empty(t, logOutput,
			"No error should be logged when FileCompleted is received for a download without a sync job. "+
				"This is expected behavior. Got log output: %s", logOutput)
	})
}

func TestFilesyncController_CategoryChanged(t *testing.T) {
	t.Run("soft-deletes sync job and files when category changes to untracked", func(t *testing.T) {
		// When a download's category changes to a category that has NO enabled app,
		// the sync job and sync files should be soft-deleted. This allows reactivation
		// if the torrent returns to a tracked category later.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		downloaderName := gofakeit.Noun()
		trackedCategory := gofakeit.Noun()
		untrackedCategory := gofakeit.Noun() + "_untracked"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the tracked category
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(trackedCategory).
			SetDownloadsPath(t.TempDir()).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// No app for the untracked category

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(trackedCategory).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create sync job
		sj, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase("/downloads").
			SetLocalBase("/local").
			Save(ctx)
		require.NoError(t, err)

		// Create download file
		df, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath("test.mkv").
			SetSize(1000).
			Save(ctx)
		require.NoError(t, err)

		// Create sync file
		_, err = db.SyncFile.Create().
			SetSyncJobID(sj.ID).
			SetDownloadFileID(df.ID).
			SetRelativePath("test.mkv").
			SetSize(1000).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Verify sync job and file exist before category change
		syncJobCount, err := db.SyncJob.Query().Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, syncJobCount, "sync job should exist before category change")

		syncFileCount, err := db.SyncFile.Query().Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, syncFileCount, "sync file should exist before category change")

		// Change category to untracked (simulate what download.Controller does)
		dl, err = dl.Update().
			SetPreviousCategory(trackedCategory).
			SetCategory(untrackedCategory).
			Save(ctx)
		require.NoError(t, err)

		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
		})
		time.Sleep(100 * time.Millisecond)

		// Verify sync job is soft-deleted (not visible in normal queries)
		syncJobCount, err = db.SyncJob.Query().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, syncJobCount,
			"sync job should be soft-deleted when category changes to untracked")

		// Verify sync files are soft-deleted (not visible in normal queries)
		syncFileCount, err = db.SyncFile.Query().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, syncFileCount,
			"sync files should be soft-deleted when category changes to untracked")
	})

	t.Run("reactivates soft-deleted sync job and files when category changes back to tracked", func(t *testing.T) {
		// When a download's category changes back to a tracked category,
		// any soft-deleted sync job and files should be reactivated.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		downloaderName := gofakeit.Noun()
		trackedCategory := gofakeit.Noun()
		untrackedCategory := gofakeit.Noun() + "_untracked"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the tracked category
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(trackedCategory).
			SetDownloadsPath(t.TempDir()).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(trackedCategory).
			SetSavePath("/downloads").
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create sync job
		sj, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase("/downloads").
			SetLocalBase("/local").
			Save(ctx)
		require.NoError(t, err)
		originalSyncJobID := sj.ID

		// Create download file
		df, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath("test.mkv").
			SetSize(1000).
			Save(ctx)
		require.NoError(t, err)

		// Create sync file
		sf, err := db.SyncFile.Create().
			SetSyncJobID(sj.ID).
			SetDownloadFileID(df.ID).
			SetRelativePath("test.mkv").
			SetSize(1000).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)
		originalSyncFileID := sf.ID

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Change category to untracked (soft-deletes)
		dl, err = dl.Update().
			SetPreviousCategory(trackedCategory).
			SetCategory(untrackedCategory).
			Save(ctx)
		require.NoError(t, err)

		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
		})
		time.Sleep(100 * time.Millisecond)

		// Verify soft-deleted
		syncJobCount, err := db.SyncJob.Query().Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 0, syncJobCount, "sync job should be soft-deleted")

		// Change category back to tracked (should reactivate)
		dl, err = dl.Update().
			SetPreviousCategory(untrackedCategory).
			SetCategory(trackedCategory).
			Save(ctx)
		require.NoError(t, err)

		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
		})
		time.Sleep(100 * time.Millisecond)

		// Verify sync job is reactivated with same ID
		reactivatedSJ, err := db.SyncJob.Query().Only(ctx)
		require.NoError(t, err, "sync job should be reactivated")
		assert.Equal(t, originalSyncJobID, reactivatedSJ.ID,
			"reactivated sync job should have same ID")

		// Verify sync file is reactivated with same ID
		reactivatedSF, err := db.SyncFile.Query().Only(ctx)
		require.NoError(t, err, "sync file should be reactivated")
		assert.Equal(t, originalSyncFileID, reactivatedSF.ID,
			"reactivated sync file should have same ID")
	})

	t.Run("does not modify download category fields", func(t *testing.T) {
		// This test exposes the bug: filesync.Controller should NOT update
		// the download's category fields because download.Controller owns that model.
		// The CategoryChanged event is published AFTER download.Controller has
		// already persisted the category change.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		oldCategory := gofakeit.Noun()
		newCategory := gofakeit.Noun()

		// Create a downloader in the database
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with previous category
		// Simulating what download.Controller does when it discovers a download
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(oldCategory).
			SetPreviousCategory(oldCategory).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Simulate download.Controller updating category
		// This is what download.Controller does BEFORE publishing CategoryChanged
		dl, err = dl.Update().SetCategory(newCategory).Save(ctx)
		require.NoError(t, err)

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish CategoryChanged event (simulating what download.Controller does)
		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
			Data: map[string]any{
				"old_category": oldCategory,
				"new_category": newCategory,
			},
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify the download's PreviousCategory was NOT modified
		// The bug is that handleCategoryChanged sets PreviousCategory = newCategory
		// which is wrong because PreviousCategory should remain the original
		updated, err := db.DownloadJob.Get(ctx, dl.ID)
		require.NoError(t, err)

		// PreviousCategory should remain the original (the category when first changed)
		// This assertion will FAIL with the current buggy code
		assert.Equal(
			t,
			oldCategory,
			updated.PreviousCategory,
			"PreviousCategory should not be modified by filesync.Controller - it should remain the category at discovery time",
		)

		// Category should be the new one (already set by download.Controller)
		assert.Equal(t, newCategory, updated.Category,
			"Category should remain as set by download.Controller")
	})

	t.Run("does not emit AppNotifyStarted when no app for new category", func(t *testing.T) {
		// When no app is configured for the new category, filesync should
		// cleanup files but not emit app-related events

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		oldCategory := gofakeit.Noun()
		untrackedCategory := gofakeit.Noun() + "_untracked"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(untrackedCategory).
			SetPreviousCategory(oldCategory).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the OLD category with cleanup enabled
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(oldCategory).
			SetDownloadsPath(t.TempDir()).
			SetCleanupOnCategoryChange(true).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// No app for the new "untracked" category

		// Track events
		var receivedEvents []events.Type
		sub := bus.Subscribe(events.Cleanup, events.AppNotifyStarted)
		go func() {
			for event := range sub {
				receivedEvents = append(receivedEvents, event.Type)
			}
		}()

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish CategoryChanged to untracked category
		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
			Data: map[string]any{
				"old_category": oldCategory,
				"new_category": untrackedCategory,
			},
		})

		time.Sleep(100 * time.Millisecond)
		bus.Unsubscribe(sub)

		// Should emit Cleanup if files exist, but should NOT emit AppNotifyStarted
		// since no app cares about the new category
		for _, evt := range receivedEvents {
			assert.NotEqual(t, events.AppNotifyStarted, evt,
				"Should not emit AppNotifyStarted when no app cares about new category")
		}
	})
}

func TestFilesyncController_SyncFileStarted(t *testing.T) {
	t.Run("emits sync.file.started event when file transfer begins", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()
		tmpDir := t.TempDir()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		category := gofakeit.Noun()
		relativePath := fmt.Sprintf("%s/%s.mkv", gofakeit.Adjective()+gofakeit.Noun(), gofakeit.Verb())
		fileSize := int64(gofakeit.IntRange(100, 2000))
		savePath := "/downloads/" + gofakeit.Adverb() + gofakeit.Verb()

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath(savePath).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync job for the download
		syncJob, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase(savePath).
			SetLocalBase(tmpDir).
			Save(ctx)
		require.NoError(t, err)

		// Create a download file record (required for SyncFile FK)
		downloadFile, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync file record
		sf, err := db.SyncFile.Create().
			SetSyncJobID(syncJob.ID).
			SetDownloadFileID(downloadFile.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)

		// Track if sync.file.started event is received
		var syncFileStartedReceived atomic.Bool
		var receivedFilePath string
		var mu sync.Mutex
		sub := bus.Subscribe(events.SyncFileStarted)
		go func() {
			for evt := range sub {
				syncFileStartedReceived.Store(true)
				mu.Lock()
				receivedFilePath, _ = evt.Data["file_path"].(string)
				mu.Unlock()
			}
		}()

		// Create mock transferer that simulates successful transfer
		mockTransfer := internaltesting.NewMockTransferer()

		// Start the controller with the mock transferer
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
			filesync.WithControllerTransferer(mockTransfer),
			filesync.WithControllerSyncingPath(tmpDir),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Emit SyncFileCreated to trigger the file transfer
		bus.Publish(events.Event{
			Type:    events.SyncFileCreated,
			Subject: dl,
			Data: map[string]any{
				"sync_job_id":  syncJob.ID.String(),
				"sync_file_id": sf.ID.String(),
				"file_path":    sf.RelativePath,
				"file_size":    sf.Size,
			},
		})

		// Wait for event processing
		time.Sleep(500 * time.Millisecond)
		bus.Unsubscribe(sub)

		// Verify sync.file.started event was emitted
		assert.True(t, syncFileStartedReceived.Load(),
			"sync.file.started event should be emitted when file transfer begins")

		mu.Lock()
		assert.Equal(t, relativePath, receivedFilePath,
			"sync.file.started event should include the file path")
		mu.Unlock()
	})
}

func TestFilesyncController_SkipsTransferWhenFileAlreadyExists(t *testing.T) {
	// Test that files are not re-downloaded when they already exist either in:
	// 1. Final destination - files previously synced but DB was reset
	// 2. Staging directory - partially completed syncs being resumed

	tests := []struct {
		name     string
		location string // "final_destination" or "staging"
	}{
		{
			name:     "skips transfer when file exists in final destination",
			location: "final_destination",
		},
		{
			name:     "skips transfer when file exists in staging directory",
			location: "staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := events.New()
			defer bus.Close()

			db := internaltesting.NewTestDB(t)
			ctx := context.Background()
			syncingDir := t.TempDir()   // Staging directory
			downloadsDir := t.TempDir() // Final destination

			// Generate random test data
			downloaderName := gofakeit.Noun()
			category := gofakeit.Noun()
			downloadName := gofakeit.MovieName()
			// qBittorrent file paths include the torrent folder name
			// e.g., for torrent "MyShow.S01", files are "MyShow.S01/episode.mkv"
			relativePath := fmt.Sprintf("%s/%s.mkv", downloadName, gofakeit.Verb())
			fileSize := int64(gofakeit.IntRange(100, 2000))
			savePath := "/downloads/" + gofakeit.Adverb() + gofakeit.Verb()

			// Create a downloader
			dlr, err := db.DownloadClient.Create().
				SetName(downloaderName).
				SetType("qbittorrent").
				SetURL("http://localhost:8080").
				SetEnabled(true).
				Save(ctx)
			require.NoError(t, err)

			// Create an app for the category with a downloads path
			_, err = db.App.Create().
				SetName(gofakeit.AppName()).
				SetType(app.TypeSonarr).
				SetCategory(category).
				SetDownloadsPath(downloadsDir).
				SetEnabled(true).
				Save(ctx)
			require.NoError(t, err)

			// Create a download job (simulating discovery)
			dl, err := db.DownloadJob.Create().
				SetRemoteID(gofakeit.UUID()).
				SetName(downloadName).
				SetDownloadClientID(dlr.ID).
				SetCategory(category).
				SetSavePath(savePath).
				SetDiscoveredAt(time.Now()).
				Save(ctx)
			require.NoError(t, err)

			// Create a sync job (don't set LocalBase so controller constructs it with job_ prefix)
			syncJob, err := db.SyncJob.Create().
				SetDownloadJobID(dl.ID).
				SetRemoteBase(savePath).
				Save(ctx)
			require.NoError(t, err)

			// Create download file record
			downloadFile, err := db.DownloadFile.Create().
				SetDownloadJobID(dl.ID).
				SetRelativePath(relativePath).
				SetSize(fileSize).
				Save(ctx)
			require.NoError(t, err)

			// Create sync file record
			sf, err := db.SyncFile.Create().
				SetSyncJobID(syncJob.ID).
				SetDownloadFileID(downloadFile.ID).
				SetRelativePath(relativePath).
				SetSize(fileSize).
				SetStatus(syncfile.StatusPending).
				Save(ctx)
			require.NoError(t, err)

			// Create the file in the appropriate location based on test case
			var existingFilePath string
			switch tt.location {
			case "final_destination":
				// File in final destination (simulates previously synced file with DB wiped)
				// relativePath already includes the download folder name (like qBittorrent returns)
				existingFilePath = filepath.Join(downloadsDir, sf.RelativePath)
			case "staging":
				// File in staging directory (simulates resumed sync)
				// Use the same path format as the controller: syncingPath/job_<id>/relativePath
				existingFilePath = filepath.Join(
					syncingDir,
					"job_"+syncJob.ID.String(),
					sf.RelativePath,
				)
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(existingFilePath), 0750))
			require.NoError(t, os.WriteFile(existingFilePath, make([]byte, fileSize), 0600))

			// Create mock transferer to track calls
			mockTransfer := internaltesting.NewMockTransferer()

			// Start the controller
			c := filesync.NewController(
				bus,
				db,
				filesync.WithControllerLogger(zerolog.Nop()),
				filesync.WithControllerTransferer(mockTransfer),
				filesync.WithControllerSyncingPath(syncingDir),
				filesync.WithControllerDownloadsPath(downloadsDir),
			)
			require.NoError(t, c.Start(ctx))
			defer c.Stop()

			// Track SyncFileComplete events
			var syncFileCompleteReceived atomic.Bool
			var alreadySynced atomic.Bool
			sub := bus.Subscribe(events.SyncFileComplete)
			go func() {
				for evt := range sub {
					syncFileCompleteReceived.Store(true)
					if v, ok := evt.Data["already_synced"].(bool); ok && v {
						alreadySynced.Store(true)
					}
				}
			}()

			// Emit SyncFileCreated to trigger file transfer check
			bus.Publish(events.Event{
				Type:    events.SyncFileCreated,
				Subject: dl,
				Data: map[string]any{
					"sync_job_id":  syncJob.ID.String(),
					"sync_file_id": sf.ID.String(),
					"file_path":    sf.RelativePath,
					"file_size":    sf.Size,
				},
			})

			// Wait for event processing
			time.Sleep(500 * time.Millisecond)
			bus.Unsubscribe(sub)

			// Transfer should NOT have been called because file already exists
			transferCalls := mockTransfer.GetTransferCalls()
			assert.Empty(t, transferCalls,
				"Transfer should not be called when file already exists in %s. "+
					"Got %d transfer calls", tt.location, len(transferCalls))

			// File should be marked as complete with already_synced flag
			assert.True(t, syncFileCompleteReceived.Load(),
				"SyncFileComplete event should be emitted")
			assert.True(t, alreadySynced.Load(),
				"SyncFileComplete event should have already_synced=true flag")

			// Verify the sync file status is complete
			updatedSyncFiles, err := db.SyncFile.Query().
				Where(syncfile.SyncJobIDEQ(syncJob.ID)).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, updatedSyncFiles, 1)
			assert.Equal(t, syncfile.StatusComplete, updatedSyncFiles[0].Status,
				"Sync file should be marked as complete")
		})
	}
}

func TestFilesyncController_SyncStarted(t *testing.T) {
	t.Run("emits sync.started event when first file transfer begins", func(t *testing.T) {
		// This test exposes a bug: sync.started event is never emitted because
		// the database auto-populates started_at with the current time on INSERT,
		// so syncJob.StartedAt.Time.IsZero() is always false.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()
		tmpDir := t.TempDir()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		category := gofakeit.Noun()
		relativePath := fmt.Sprintf("%s/%s.mkv", gofakeit.Adjective()+gofakeit.Noun(), gofakeit.Verb())
		fileSize := int64(gofakeit.IntRange(100, 2000))
		savePath := "/downloads/" + gofakeit.Adverb() + gofakeit.Verb()

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath(savePath).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync job for the download
		syncJob, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase(savePath).
			SetLocalBase(tmpDir).
			Save(ctx)
		require.NoError(t, err)

		// Create a download file record (required for SyncFile FK)
		downloadFile, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync file record
		sf, err := db.SyncFile.Create().
			SetSyncJobID(syncJob.ID).
			SetDownloadFileID(downloadFile.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)

		// Track if sync.started event is received
		var syncStartedReceived atomic.Bool
		sub := bus.Subscribe(events.SyncStarted)
		go func() {
			for range sub {
				syncStartedReceived.Store(true)
			}
		}()

		// Create mock transferer that simulates successful transfer
		mockTransfer := internaltesting.NewMockTransferer()

		// Start the controller with the mock transferer
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
			filesync.WithControllerTransferer(mockTransfer),
			filesync.WithControllerSyncingPath(tmpDir),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Emit SyncFileCreated to trigger the file transfer
		bus.Publish(events.Event{
			Type:    events.SyncFileCreated,
			Subject: dl,
			Data: map[string]any{
				"sync_job_id":  syncJob.ID.String(),
				"sync_file_id": sf.ID.String(),
				"file_path":    sf.RelativePath,
				"file_size":    sf.Size,
			},
		})

		// Wait for event processing
		time.Sleep(500 * time.Millisecond)
		bus.Unsubscribe(sub)

		// Verify sync.started event was emitted
		// THIS TEST WILL FAIL due to the bug: started_at is auto-populated by DB
		assert.True(t, syncStartedReceived.Load(),
			"sync.started event should be emitted when first file transfer begins")
	})
}

func TestFilesyncController_DefaultPathIncludesDownloaderName(t *testing.T) {
	t.Run("final path includes downloader name when using default path", func(t *testing.T) {
		// Bug: When an app doesn't have a custom downloads_path, the fallback
		// should be $baseDownloadsPath/$downloaderName/$category, but currently
		// it's just $baseDownloadsPath/$category (missing downloaderName).

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()
		tmpDir := t.TempDir()

		// Set up directories
		syncingPath := filepath.Join(tmpDir, "syncing")
		downloadsPath := filepath.Join(tmpDir, "downloads")
		require.NoError(t, os.MkdirAll(syncingPath, 0755))

		// Generate random test data
		downloaderName := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		category := gofakeit.Noun()
		fileName := gofakeit.Noun() + ".mkv"
		fileSize := int64(gofakeit.IntRange(100, 2000))
		savePath := "/remote/downloads"

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app WITHOUT a custom downloads path
		// This forces the controller to use the global downloads path fallback
		_, err = db.App.Create().
			SetName("testapp").
			SetType(app.TypePassthrough).
			SetCategory(category).
			SetDownloadsPath(""). // Empty - forces use of global path
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath(savePath).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync job
		syncJob, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase(savePath).
			SetLocalBase(filepath.Join(syncingPath, "job_"+dl.ID.String())).
			Save(ctx)
		require.NoError(t, err)

		// Create a download file
		downloadFile, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath(fileName).
			SetSize(fileSize).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync file (already complete)
		_, err = db.SyncFile.Create().
			SetSyncJobID(syncJob.ID).
			SetDownloadFileID(downloadFile.ID).
			SetRelativePath(fileName).
			SetSize(fileSize).
			SetSyncedSize(fileSize).
			SetStatus(syncfile.StatusComplete).
			Save(ctx)
		require.NoError(t, err)

		// Create the local synced file (structure matches what syncer creates)
		// The syncer puts files at localBase/downloadName/relativePath
		localSyncBase := syncJob.LocalBase
		require.NoError(t, os.MkdirAll(localSyncBase, 0755))
		localFilePath := filepath.Join(localSyncBase, fileName)
		require.NoError(t, os.WriteFile(localFilePath, []byte("test content"), 0644))

		// Track the move.complete event to verify final_path
		var receivedFinalPath string
		var mu sync.Mutex
		sub := bus.Subscribe(events.MoveComplete)
		go func() {
			for evt := range sub {
				mu.Lock()
				receivedFinalPath, _ = evt.Data["final_path"].(string)
				mu.Unlock()
			}
		}()

		// Start the controller with global downloads path
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
			filesync.WithControllerSyncingPath(syncingPath),
			filesync.WithControllerDownloadsPath(downloadsPath),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Emit SyncComplete to trigger the move operation
		bus.Publish(events.Event{
			Type:    events.SyncComplete,
			Subject: dl,
			Data: map[string]any{
				"sync_job_id": syncJob.ID.String(),
			},
		})

		// Wait for event processing
		time.Sleep(200 * time.Millisecond)
		bus.Unsubscribe(sub)

		// The final path SHOULD include the downloader name:
		// $downloadsPath/$downloaderName/$category/$downloadName
		// But currently it's: $downloadsPath/$category/$downloadName (missing downloaderName)
		expectedPath := filepath.Join(downloadsPath, downloaderName, category, downloadName)

		mu.Lock()
		actualPath := receivedFinalPath
		mu.Unlock()

		// This test will FAIL with current buggy code because it produces:
		// $downloadsPath/$category/$downloadName (missing downloaderName)
		assert.Equal(t, expectedPath, actualPath,
			"final path should include downloader name: base/downloaderName/category/downloadName")
	})
}

func TestFilesyncController_MoveCompleteEmittedWhenFileAlreadyExistsAtFinal(t *testing.T) {
	t.Run("emits move.complete when file already exists at final destination", func(t *testing.T) {
		// Bug: When a file already exists at the final destination (e.g., DB was wiped
		// but files remain), the sync is skipped correctly but MoveComplete is never
		// emitted because local_base is empty. This means apps are never notified to
		// import the file.
		//
		// Expected behavior: When all files already exist at the final destination,
		// MoveComplete should be emitted so apps can import them.

		bus := events.New()
		defer bus.Close()

		db := internaltesting.NewTestDB(t)
		ctx := context.Background()
		syncingDir := t.TempDir()
		downloadsDir := t.TempDir()

		// Generate random test data
		downloaderName := gofakeit.Noun()
		category := gofakeit.Noun()
		downloadName := gofakeit.MovieName()
		// qBittorrent file paths include the torrent folder name
		relativePath := fmt.Sprintf("%s/%s.mkv", downloadName, gofakeit.Verb())
		fileSize := int64(gofakeit.IntRange(100, 2000))
		savePath := "/downloads/" + gofakeit.Adverb() + gofakeit.Verb()

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName(downloaderName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the category with a downloads path
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetDownloadsPath(downloadsDir).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(downloadName).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetSavePath(savePath).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync job (LocalBase will be empty since no files transferred)
		syncJob, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase(savePath).
			Save(ctx)
		require.NoError(t, err)

		// Create download file record
		downloadFile, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			Save(ctx)
		require.NoError(t, err)

		// Create sync file record
		sf, err := db.SyncFile.Create().
			SetSyncJobID(syncJob.ID).
			SetDownloadFileID(downloadFile.ID).
			SetRelativePath(relativePath).
			SetSize(fileSize).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)

		// Create the file at the FINAL destination (simulating DB wipe with files intact)
		finalFilePath := filepath.Join(downloadsDir, sf.RelativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(finalFilePath), 0750))
		require.NoError(t, os.WriteFile(finalFilePath, make([]byte, fileSize), 0600))

		// Track events
		var moveCompleteReceived atomic.Bool
		var receivedFinalPath string
		var mu sync.Mutex
		sub := bus.Subscribe(events.MoveComplete)
		go func() {
			for evt := range sub {
				moveCompleteReceived.Store(true)
				mu.Lock()
				receivedFinalPath, _ = evt.Data["final_path"].(string)
				mu.Unlock()
			}
		}()

		// Create mock transferer to verify no transfer is called
		mockTransfer := internaltesting.NewMockTransferer()

		// Start the controller
		c := filesync.NewController(
			bus,
			db,
			filesync.WithControllerLogger(zerolog.Nop()),
			filesync.WithControllerTransferer(mockTransfer),
			filesync.WithControllerSyncingPath(syncingDir),
			filesync.WithControllerDownloadsPath(downloadsDir),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Emit SyncFileCreated to trigger file transfer check
		bus.Publish(events.Event{
			Type:    events.SyncFileCreated,
			Subject: dl,
			Data: map[string]any{
				"sync_job_id":  syncJob.ID.String(),
				"sync_file_id": sf.ID.String(),
				"file_path":    sf.RelativePath,
				"file_size":    sf.Size,
			},
		})

		// Wait for event processing
		time.Sleep(500 * time.Millisecond)
		bus.Unsubscribe(sub)

		// Transfer should NOT have been called
		assert.Empty(t, mockTransfer.GetTransferCalls(),
			"Transfer should not be called when file already exists at final destination")

		// MoveComplete SHOULD be emitted (this is the bug - currently it's not)
		assert.True(t, moveCompleteReceived.Load(),
			"MoveComplete should be emitted when files already exist at final destination")

		// Verify the final path is correct
		mu.Lock()
		actualPath := receivedFinalPath
		mu.Unlock()
		expectedPath := filepath.Join(downloadsDir, downloadName)
		assert.Equal(t, expectedPath, actualPath,
			"MoveComplete should contain the correct final path")
	})
}
