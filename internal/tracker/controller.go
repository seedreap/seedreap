// Package tracker maintains the high-level TrackedDownload state by watching
// events from the download pipeline. It provides accurate workflow state for
// stats and UI display.
package tracker

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadjob"
	"github.com/seedreap/seedreap/internal/ent/generated/syncfile"
	"github.com/seedreap/seedreap/internal/ent/generated/syncjob"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	"github.com/seedreap/seedreap/internal/ent/mixins"
	"github.com/seedreap/seedreap/internal/events"
)

// Controller watches events and maintains TrackedDownload state.
// It subscribes to all relevant events and updates the TrackedDownload
// record to reflect the current workflow state.
type Controller struct {
	eventBus *events.Bus
	db       *generated.Client
	logger   zerolog.Logger

	subscription events.Subscription
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// ControllerOption is a functional option for configuring the Controller.
type ControllerOption func(*Controller)

// WithControllerLogger sets the logger for the controller.
func WithControllerLogger(logger zerolog.Logger) ControllerOption {
	return func(c *Controller) {
		c.logger = logger
	}
}

// NewController creates a new tracker Controller.
func NewController(
	eventBus *events.Bus,
	db *generated.Client,
	opts ...ControllerOption,
) *Controller {
	c := &Controller{
		eventBus: eventBus,
		db:       db,
		logger:   zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start begins watching events.
func (c *Controller) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)

	// Subscribe to all events that affect tracked download state
	c.subscription = c.eventBus.Subscribe(
		// Download lifecycle events
		events.DownloadDiscovered,
		events.DownloadUpdated,
		events.DownloadPaused,
		events.DownloadResumed,
		events.DownloadComplete,
		events.DownloadError,
		events.DownloadRemoved,
		events.CategoryChanged,
		// Sync events
		events.SyncStarted,
		events.SyncFileComplete,
		events.SyncComplete,
		events.SyncFailed,
		events.SyncCancelled,
		// Move events
		events.MoveStarted,
		events.MoveComplete,
		events.MoveFailed,
		// App notification events
		events.AppNotifyStarted,
		events.AppNotifyComplete,
		events.AppNotifyFailed,
	)

	c.wg.Add(1)
	go c.run(ctx)

	c.logger.Info().Msg("tracker controller started")
	return nil
}

// Stop stops the controller and waits for it to finish.
func (c *Controller) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	if c.subscription != nil {
		c.eventBus.Unsubscribe(c.subscription)
	}
	c.logger.Info().Msg("tracker controller stopped")
	return nil
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.subscription:
			if !ok {
				return
			}
			c.handleEvent(ctx, event)
		}
	}
}

func (c *Controller) handleEvent(ctx context.Context, event events.Event) {
	switch event.Type {
	// Download lifecycle
	case events.DownloadDiscovered:
		c.handleDownloadDiscovered(ctx, event)
	case events.DownloadUpdated:
		c.handleDownloadUpdated(ctx, event)
	case events.DownloadPaused:
		c.handleStateChange(ctx, event, trackeddownload.StatePaused)
	case events.DownloadResumed:
		c.handleDownloadResumed(ctx, event)
	case events.DownloadComplete:
		c.handleDownloadComplete(ctx, event)
	case events.DownloadError:
		c.handleStateChange(ctx, event, trackeddownload.StateError)
	case events.DownloadRemoved:
		c.handleDownloadRemoved(ctx, event)
	case events.CategoryChanged:
		c.handleCategoryChanged(ctx, event)
	// Sync events
	case events.SyncStarted:
		c.handleSyncStarted(ctx, event)
	case events.SyncFileComplete:
		c.handleSyncFileComplete(ctx, event)
	case events.SyncComplete:
		c.handleStateChange(ctx, event, trackeddownload.StateSynced)
	case events.SyncFailed:
		c.handleStateChange(ctx, event, trackeddownload.StateSyncError)
	case events.SyncCancelled:
		c.handleStateChange(ctx, event, trackeddownload.StateCancelled)
	// Move events
	case events.MoveStarted:
		c.handleStateChange(ctx, event, trackeddownload.StateMoving)
	case events.MoveComplete:
		c.handleStateChange(ctx, event, trackeddownload.StateMoved)
	case events.MoveFailed:
		c.handleStateChange(ctx, event, trackeddownload.StateMoveError)
	// App notification events
	case events.AppNotifyStarted:
		c.handleStateChange(ctx, event, trackeddownload.StateImporting)
	case events.AppNotifyComplete:
		c.handleAppNotifyComplete(ctx, event)
	case events.AppNotifyFailed:
		c.handleStateChange(ctx, event, trackeddownload.StateImportError)
	default:
		// Other events are subscribed but don't require tracked download updates
	}
}

func (c *Controller) handleDownloadDiscovered(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	// Check if there's an enabled app for this download's category
	// If not, don't create a tracked download - no point tracking downloads that no app will process
	appExists, err := c.db.App.Query().
		Where(
			app.CategoryEQ(dlJob.Category),
			app.EnabledEQ(true),
		).
		Exist(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("download", dlJob.Name).
			Str("category", dlJob.Category).
			Msg("failed to check for app with category")
		return
	}
	if !appExists {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Str("category", dlJob.Category).
			Msg("no enabled app for category, skipping tracked download creation")
		return
	}

	// Check if tracked download already exists
	existing, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err == nil && existing != nil {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Msg("tracked download already exists")
		return
	}

	// Get app name for this category
	appName := c.getAppNameForCategory(ctx, dlJob.Category)

	// Determine initial state
	var state trackeddownload.State
	switch dlJob.Status {
	case downloadjob.StatusComplete:
		state = trackeddownload.StatePending
	case downloadjob.StatusPaused:
		state = trackeddownload.StatePaused
	case downloadjob.StatusError:
		state = trackeddownload.StateError
	default:
		state = trackeddownload.StateDownloading
	}

	// Count files from event data
	fileCount, _ := event.Data["file_count"].(int)

	_, createErr := c.db.TrackedDownload.Create().
		SetDownloadJobID(dlJob.ID).
		SetName(dlJob.Name).
		SetCategory(dlJob.Category).
		SetAppName(appName).
		SetState(state).
		SetTotalSize(dlJob.Size).
		SetTotalFiles(fileCount).
		SetDiscoveredAt(dlJob.DiscoveredAt).
		Save(ctx)
	if createErr != nil {
		c.logger.Error().Err(createErr).
			Str("download", dlJob.Name).
			Msg("failed to create tracked download")
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("state", string(state)).
		Msg("created tracked download")
}

func (c *Controller) handleDownloadUpdated(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return // Not tracking this download
	}

	// Update size if changed
	if dlJob.Size != td.TotalSize {
		_, updateErr := td.Update().
			SetTotalSize(dlJob.Size).
			Save(ctx)
		if updateErr != nil {
			c.logger.Error().Err(updateErr).
				Str("download", dlJob.Name).
				Msg("failed to update tracked download size")
		}
	}
}

func (c *Controller) handleDownloadResumed(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return
	}

	// Check if we're also syncing (downloading_syncing state)
	sj, syncErr := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	var state trackeddownload.State
	if syncErr == nil && sj != nil && sj.Status == syncjob.StatusSyncing {
		state = trackeddownload.StateDownloadingSyncing
	} else {
		state = trackeddownload.StateDownloading
	}

	_, updateErr := td.Update().
		SetState(state).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download state")
	}
}

func (c *Controller) handleDownloadComplete(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return
	}

	// Check sync job status to determine state
	var state trackeddownload.State
	sj, syncErr := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if syncErr == nil && sj != nil {
		switch sj.Status {
		case syncjob.StatusSyncing:
			state = trackeddownload.StateSyncing
		case syncjob.StatusComplete:
			state = trackeddownload.StateSynced
		case syncjob.StatusPending:
			state = trackeddownload.StatePending
		default:
			state = trackeddownload.StatePending
		}
	} else {
		state = trackeddownload.StatePending
	}

	_, updateErr := td.Update().
		SetState(state).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download state")
	}
}

func (c *Controller) handleDownloadRemoved(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return // Not tracking this download
	}

	if deleteErr := c.db.TrackedDownload.DeleteOne(td).Exec(ctx); deleteErr != nil {
		c.logger.Error().Err(deleteErr).
			Str("download", dlJob.Name).
			Msg("failed to delete tracked download")
	}
}

func (c *Controller) handleCategoryChanged(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	appName := c.getAppNameForCategory(ctx, dlJob.Category)

	// Try to find an active tracked download first
	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)

	if err != nil {
		// No active tracked download found - check for soft-deleted one to reactivate
		if appName != "" {
			c.reactivateTrackedDownload(ctx, dlJob, appName)
		}
		return
	}

	// If no app for new category, soft-delete the tracked download
	if appName == "" {
		c.logger.Info().
			Str("download", dlJob.Name).
			Str("new_category", dlJob.Category).
			Msg("category changed to untracked, soft-deleting tracked download")

		if deleteErr := c.db.TrackedDownload.DeleteOneID(td.ID).Exec(ctx); deleteErr != nil {
			c.logger.Error().Err(deleteErr).
				Str("download", dlJob.Name).
				Msg("failed to soft-delete tracked download on category change to untracked")
		}
		return
	}

	_, updateErr := td.Update().
		SetCategory(dlJob.Category).
		SetAppName(appName).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download category")
	}
}

// reactivateTrackedDownload reactivates a soft-deleted tracked download.
func (c *Controller) reactivateTrackedDownload(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	appName string,
) {
	// Query including soft-deleted records
	softDeleteCtx := mixins.SkipSoftDelete(ctx)
	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(softDeleteCtx)
	if err != nil {
		// No soft-deleted record found either, nothing to reactivate
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("new_category", dlJob.Category).
		Str("app_name", appName).
		Msg("reactivating soft-deleted tracked download")

	// Reactivate by clearing deleted_at and updating fields
	// Use UpdateOneID to avoid re-querying the record which would be filtered by soft-delete
	_, updateErr := c.db.TrackedDownload.UpdateOneID(td.ID).
		ClearDeletedAt().
		SetCategory(dlJob.Category).
		SetAppName(appName).
		Save(softDeleteCtx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to reactivate tracked download")
	}
}

func (c *Controller) handleSyncStarted(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return
	}

	// Check if download is still in progress (downloading_syncing state)
	var state trackeddownload.State
	if dlJob.Status == downloadjob.StatusDownloading {
		state = trackeddownload.StateDownloadingSyncing
	} else {
		state = trackeddownload.StateSyncing
	}

	_, updateErr := td.Update().
		SetState(state).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download state to syncing")
	}
}

func (c *Controller) handleSyncFileComplete(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return
	}

	// Update completed size from sync files
	sj, syncErr := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if syncErr != nil {
		return
	}

	syncFiles, filesErr := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		All(ctx)
	if filesErr != nil {
		return
	}

	var completedSize int64
	for _, f := range syncFiles {
		completedSize += f.SyncedSize
	}

	_, updateErr := td.Update().
		SetCompletedSize(completedSize).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download completed size")
	}
}

func (c *Controller) handleAppNotifyComplete(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		return
	}

	_, updateErr := td.Update().
		SetState(trackeddownload.StateImported).
		SetCompletedAt(event.Timestamp).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to update tracked download to imported")
	}
}

func (c *Controller) handleStateChange(
	ctx context.Context,
	event events.Event,
	state trackeddownload.State,
) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		return
	}

	td, err := c.db.TrackedDownload.Query().
		Where(trackeddownload.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		// Only log if it's not a "not found" error
		if !generated.IsNotFound(err) {
			c.logger.Error().Err(err).
				Str("download", dlJob.Name).
				Msg("failed to get tracked download")
		}
		return
	}

	update := td.Update().SetState(state)

	// Get error message if this is an error state
	if state == trackeddownload.StateSyncError ||
		state == trackeddownload.StateMoveError ||
		state == trackeddownload.StateImportError ||
		state == trackeddownload.StateError {
		if errMsg, ok := event.Data["error"].(string); ok {
			update.SetErrorMessage(errMsg)
		}
	}

	_, updateErr := update.Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Str("state", string(state)).
			Msg("failed to update tracked download state")
	}
}

func (c *Controller) getAppNameForCategory(ctx context.Context, category string) string {
	if category == "" {
		return ""
	}

	apps, err := c.db.App.Query().
		Where(
			app.CategoryEQ(category),
			app.EnabledEQ(true),
		).
		All(ctx)
	if err != nil || len(apps) == 0 {
		return ""
	}

	return apps[0].Name
}
