package tracker_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadjob"
	"github.com/seedreap/seedreap/internal/ent/generated/syncfile"
	"github.com/seedreap/seedreap/internal/ent/generated/syncjob"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	"github.com/seedreap/seedreap/internal/events"
	testpkg "github.com/seedreap/seedreap/internal/testing"
	"github.com/seedreap/seedreap/internal/tracker"
)

func TestController_DownloadDiscovered(t *testing.T) {
	t.Run("does not create tracked download when no app for category", func(t *testing.T) {
		// The tracker controller should NOT create tracked downloads for downloads
		// that don't have an enabled app for their category.

		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		// Create a downloader
		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a download job with a category that has NO matching app
		unmatchedCategory := gofakeit.Noun() + "_unmatched"
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(unmatchedCategory).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// NO app is created for unmatchedCategory

		// Start controller
		c := tracker.NewController(
			bus,
			db,
			tracker.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish discovery event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify NO tracked download was created
		tds, err := db.TrackedDownload.Query().All(ctx)
		require.NoError(t, err)
		assert.Empty(t, tds,
			"tracked download should NOT be created when no app exists for the download's category")
	})

	t.Run("creates tracked download when app exists for category", func(t *testing.T) {
		// The tracker controller SHOULD create tracked downloads for downloads
		// that have an enabled app for their category.

		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		// Create a downloader
		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create an app for the category
		category := gofakeit.Noun()
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
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Start controller
		c := tracker.NewController(
			bus,
			db,
			tracker.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish discovery event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify tracked download WAS created
		tds, err := db.TrackedDownload.Query().All(ctx)
		require.NoError(t, err)
		assert.Len(t, tds, 1,
			"tracked download should be created when an app exists for the download's category")
	})

	t.Run("does not create tracked download when app is disabled", func(t *testing.T) {
		// The tracker controller should NOT create tracked downloads for downloads
		// when the app for their category is disabled.

		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		// Create a downloader
		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create a DISABLED app for the category
		category := gofakeit.Noun()
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
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Start controller
		c := tracker.NewController(
			bus,
			db,
			tracker.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish discovery event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify NO tracked download was created
		tds, err := db.TrackedDownload.Query().All(ctx)
		require.NoError(t, err)
		assert.Empty(t, tds,
			"tracked download should NOT be created when the app for the category is disabled")
	})

	t.Run("creates tracked download with downloading state", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		// Create test data
		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetSize(int64(gofakeit.Number(1000000, 10000000))).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Start controller
		c := tracker.NewController(
			bus,
			db,
			tracker.WithControllerLogger(zerolog.Nop()),
		)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish discovery event
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
			Data: map[string]any{
				"file_count": 5,
			},
		})

		// Wait for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify tracked download was created
		td, err := db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Only(ctx)
		require.NoError(t, err)
		assert.Equal(t, dl.Name, td.Name)
		assert.Equal(t, dl.Category, td.Category)
		assert.Equal(t, trackeddownload.StateDownloading, td.State)
		assert.Equal(t, dl.Size, td.TotalSize)
		assert.Equal(t, 5, td.TotalFiles)
	})

	t.Run("creates tracked download with pending state when already complete", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusComplete).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})

		time.Sleep(100 * time.Millisecond)

		td, err := db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Only(ctx)
		require.NoError(t, err)
		assert.Equal(t, trackeddownload.StatePending, td.State)
	})

	t.Run("does not create duplicate tracked download", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Publish twice
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(100 * time.Millisecond)

		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(100 * time.Millisecond)

		// Should still only have one
		downloads, err := db.TrackedDownload.Query().All(ctx)
		require.NoError(t, err)
		assert.Len(t, downloads, 1)
	})
}

func TestController_SyncWorkflow(t *testing.T) {
	t.Run("updates state through sync workflow", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusComplete).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create sync job for sync file complete test
		_, err = db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase("/remote").
			SetLocalBase("/local").
			SetStatus(syncjob.StatusSyncing).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StatePending, td.State)

		// Sync started
		bus.Publish(events.Event{
			Type:    events.SyncStarted,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateSyncing, td.State)

		// Sync complete
		bus.Publish(events.Event{
			Type:    events.SyncComplete,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateSynced, td.State)

		// Move started
		bus.Publish(events.Event{
			Type:    events.MoveStarted,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateMoving, td.State)

		// Move complete
		bus.Publish(events.Event{
			Type:    events.MoveComplete,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateMoved, td.State)

		// App notify started
		bus.Publish(events.Event{
			Type:    events.AppNotifyStarted,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateImporting, td.State)

		// App notify complete
		bus.Publish(events.Event{
			Type:      events.AppNotifyComplete,
			Subject:   dl,
			Timestamp: time.Now(),
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateImported, td.State)
		assert.False(t, td.CompletedAt.IsZero(), "CompletedAt should be set")
	})
}

func TestController_DownloadingAndSync(t *testing.T) {
	t.Run("sets downloading_syncing state when downloading and syncing", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Download is still downloading (multi-file where some are complete)
		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// Sync starts while still downloading
		bus.Publish(events.Event{
			Type:    events.SyncStarted,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateDownloadingSyncing, td.State)
	})
}

func TestController_ErrorStates(t *testing.T) {
	t.Run("captures error message on sync failure", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusComplete).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		errorMsg := "connection refused"

		// Sync failed
		bus.Publish(events.Event{
			Type:    events.SyncFailed,
			Subject: dl,
			Data: map[string]any{
				"error": errorMsg,
			},
		})
		time.Sleep(50 * time.Millisecond)

		td, _ := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, trackeddownload.StateSyncError, td.State)
		assert.Equal(t, errorMsg, td.ErrorMessage)
	})
}

func TestController_DownloadRemoved(t *testing.T) {
	t.Run("deletes tracked download when download removed", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// Verify exists
		count, err := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Remove
		bus.Publish(events.Event{
			Type:    events.DownloadRemoved,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// Verify deleted
		count, err = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestController_CategoryChanged(t *testing.T) {
	t.Run("soft-deletes tracked download when category changes to untracked", func(t *testing.T) {
		// When a download's category changes to a category that has NO enabled app,
		// the tracked download should be soft-deleted. This allows reactivation
		// if the torrent returns to a tracked category later.

		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		trackedCategory := gofakeit.Noun()
		untrackedCategory := gofakeit.Noun() + "_untracked"

		// Create app for the tracked category (needed for discovery)
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeRadarr).
			SetCategory(trackedCategory).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// No app for the untracked category

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(trackedCategory).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// Verify tracked download exists
		count, err := db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count, "tracked download should exist before category change")

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
		time.Sleep(50 * time.Millisecond)

		// Verify tracked download is soft-deleted (not visible in normal queries)
		count, err = db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, count,
			"tracked download should be soft-deleted when category changes to untracked")
	})

	t.Run("reactivates soft-deleted tracked download when category changes back to tracked", func(t *testing.T) {
		// When a download's category changes back to a tracked category,
		// any soft-deleted tracked download should be reactivated.

		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		trackedCategory := gofakeit.Noun()
		untrackedCategory := gofakeit.Noun() + "_untracked"

		// Create app for the tracked category
		appName := gofakeit.AppName()
		_, err = db.App.Create().
			SetName(appName).
			SetType(app.TypeRadarr).
			SetCategory(trackedCategory).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(trackedCategory).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

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
		time.Sleep(50 * time.Millisecond)

		// Verify soft-deleted
		count, err := db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 0, count, "tracked download should be soft-deleted")

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
		time.Sleep(50 * time.Millisecond)

		// Verify tracked download is reactivated
		td, err := db.TrackedDownload.Query().
			Where(trackeddownload.DownloadJobIDEQ(dl.ID)).
			Only(ctx)
		require.NoError(t, err, "tracked download should be reactivated")
		assert.Equal(t, trackedCategory, td.Category)
		assert.Equal(t, appName, td.AppName)
	})

	t.Run("updates category and app name", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		oldCategory := gofakeit.Noun()
		newCategory := gofakeit.Noun()

		// Create app for OLD category (needed for discovery)
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeRadarr).
			SetCategory(oldCategory).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for new category
		appName := gofakeit.AppName()
		_, err = db.App.Create().
			SetName(appName).
			SetType(app.TypeSonarr).
			SetCategory(newCategory).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(oldCategory).
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// Change category (simulate what download.Controller does)
		dl, err = dl.Update().SetCategory(newCategory).Save(ctx)
		require.NoError(t, err)

		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		td, _ := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, newCategory, td.Category)
		assert.Equal(t, appName, td.AppName)
	})
}

func TestController_SyncFileComplete(t *testing.T) {
	t.Run("updates completed size", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()

		db := testpkg.NewTestDB(t)
		ctx := context.Background()

		dlrName := gofakeit.Noun()
		dlr, err := db.DownloadClient.Create().
			SetName(dlrName).
			SetType("qbittorrent").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app for the category
		category := gofakeit.Noun()
		_, err = db.App.Create().
			SetName(gofakeit.AppName()).
			SetType(app.TypeSonarr).
			SetCategory(category).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		dl, err := db.DownloadJob.Create().
			SetRemoteID(gofakeit.UUID()).
			SetName(gofakeit.MovieName()).
			SetDownloadClientID(dlr.ID).
			SetCategory(category).
			SetStatus(downloadjob.StatusComplete).
			SetSize(1000000).
			SetDiscoveredAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)

		// Create download files
		df1, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath("file1.mkv").
			SetSize(500000).
			Save(ctx)
		require.NoError(t, err)

		df2, err := db.DownloadFile.Create().
			SetDownloadJobID(dl.ID).
			SetRelativePath("file2.mkv").
			SetSize(500000).
			Save(ctx)
		require.NoError(t, err)

		// Create sync job and files
		sj, err := db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetRemoteBase("/remote").
			SetLocalBase("/local").
			SetStatus(syncjob.StatusSyncing).
			Save(ctx)
		require.NoError(t, err)

		_, err = db.SyncFile.Create().
			SetSyncJobID(sj.ID).
			SetDownloadFileID(df1.ID).
			SetRelativePath("file1.mkv").
			SetSize(500000).
			SetSyncedSize(500000).
			SetStatus(syncfile.StatusComplete).
			Save(ctx)
		require.NoError(t, err)

		sf2, err := db.SyncFile.Create().
			SetSyncJobID(sj.ID).
			SetDownloadFileID(df2.ID).
			SetRelativePath("file2.mkv").
			SetSize(500000).
			SetSyncedSize(0).
			SetStatus(syncfile.StatusPending).
			Save(ctx)
		require.NoError(t, err)

		c := tracker.NewController(bus, db)
		require.NoError(t, c.Start(ctx))
		defer c.Stop()

		// Discover
		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: dl,
		})
		time.Sleep(50 * time.Millisecond)

		// First file complete
		bus.Publish(events.Event{
			Type:    events.SyncFileComplete,
			Subject: dl,
			Data: map[string]any{
				"file_path": "file1.mkv",
			},
		})
		time.Sleep(50 * time.Millisecond)

		td, _ := db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, int64(500000), td.CompletedSize)

		// Second file complete
		_, err = sf2.Update().
			SetSyncedSize(500000).
			SetStatus(syncfile.StatusComplete).
			Save(ctx)
		require.NoError(t, err)

		bus.Publish(events.Event{
			Type:    events.SyncFileComplete,
			Subject: dl,
			Data: map[string]any{
				"file_path": "file2.mkv",
			},
		})
		time.Sleep(50 * time.Millisecond)

		td, _ = db.TrackedDownload.Query().Where(trackeddownload.DownloadJobIDEQ(dl.ID)).Only(ctx)
		assert.Equal(t, int64(1000000), td.CompletedSize)
	})
}
