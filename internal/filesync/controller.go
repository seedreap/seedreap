package filesync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/movejob"
	"github.com/seedreap/seedreap/internal/ent/generated/syncfile"
	"github.com/seedreap/seedreap/internal/ent/generated/syncjob"
	"github.com/seedreap/seedreap/internal/ent/mixins"
	"github.com/seedreap/seedreap/internal/events"
	"github.com/seedreap/seedreap/internal/fileutil"
	"github.com/seedreap/seedreap/internal/transfer"
)

// Default configuration values for the controller.
const (
	defaultControllerMaxConcurrent = 2
)

// Controller manages sync jobs and files based on events.
// It follows a microservice pattern: communicating only via the database and event bus,
// with no direct dependencies on other domain packages.
//
// The Controller is responsible for:
// - Creating sync jobs when downloads are discovered
// - Creating sync file records when files complete in the downloader
// - Transferring files from remote to local staging
// - Updating file/job status in the database
// - Emitting events for sync progress and completion.
type Controller struct {
	eventBus      *events.Bus
	db            *generated.Client
	logger        zerolog.Logger
	transferer    transfer.Transferer
	syncer        *Syncer // For live progress tracking
	syncingPath   string
	downloadsPath string // Global downloads path (used as fallback when app has no DownloadsPath)

	// Concurrency control
	maxConcurrent int
	semaphore     chan struct{}

	// Active transfers tracking for cancellation
	activeTransfers map[ulid.ULID]context.CancelFunc // syncJobID -> cancel func

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

// WithControllerTransferer sets the transfer backend for file transfers.
func WithControllerTransferer(t transfer.Transferer) ControllerOption {
	return func(c *Controller) {
		c.transferer = t
	}
}

// WithControllerSyncingPath sets the local staging path for synced files.
func WithControllerSyncingPath(path string) ControllerOption {
	return func(c *Controller) {
		c.syncingPath = path
	}
}

// WithControllerMaxConcurrent sets the maximum concurrent file transfers.
func WithControllerMaxConcurrent(n int) ControllerOption {
	return func(c *Controller) {
		c.maxConcurrent = n
		c.semaphore = make(chan struct{}, n)
	}
}

// WithControllerDownloadsPath sets the global downloads path (fallback when app has no DownloadsPath).
func WithControllerDownloadsPath(path string) ControllerOption {
	return func(c *Controller) {
		c.downloadsPath = path
	}
}

// WithControllerSyncer sets the syncer for live progress tracking.
func WithControllerSyncer(syncer *Syncer) ControllerOption {
	return func(c *Controller) {
		c.syncer = syncer
	}
}

// NewController creates a new filesync Controller.
func NewController(
	eventBus *events.Bus,
	db *generated.Client,
	opts ...ControllerOption,
) *Controller {
	c := &Controller{
		eventBus:        eventBus,
		db:              db,
		logger:          zerolog.Nop(),
		maxConcurrent:   defaultControllerMaxConcurrent,
		semaphore:       make(chan struct{}, defaultControllerMaxConcurrent),
		activeTransfers: make(map[ulid.ULID]context.CancelFunc),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start begins processing events.
func (c *Controller) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)

	// Subscribe to events we care about
	c.subscription = c.eventBus.Subscribe(
		events.DownloadDiscovered,
		events.FileCompleted,
		events.DownloadRemoved,
		events.CategoryChanged,
		events.SyncFileCreated, // We emit this ourselves to trigger transfers
		events.SyncComplete,    // Trigger move to final destination
	)

	c.wg.Add(1)
	go c.run(ctx)

	c.logger.Info().Msg("filesync controller started")
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
	c.logger.Info().Msg("filesync controller stopped")
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
	case events.DownloadDiscovered:
		c.handleDownloadDiscovered(ctx, event)
	case events.FileCompleted:
		c.handleFileCompleted(ctx, event)
	case events.SyncFileCreated:
		c.handleSyncFileCreated(ctx, event)
	case events.SyncComplete:
		c.handleSyncComplete(ctx, event)
	case events.DownloadRemoved:
		c.handleDownloadRemoved(ctx, event)
	case events.CategoryChanged:
		c.handleCategoryChanged(ctx, event)
	default:
		// Ignore other event types - we only subscribed to the ones above
	}
}

func (c *Controller) handleDownloadDiscovered(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	// Check if there's an enabled app for this download's category
	// If not, don't create a sync job - no point syncing files that no app will process
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
			Msg("no enabled app for category, skipping sync job creation")
		return
	}

	// Check if a sync job already exists for this download
	existingJob, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err == nil && existingJob != nil {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Str("sync_job_id", existingJob.ID.String()).
			Msg("sync job already exists for download")
		return
	}

	// Extract data from event
	savePath, _ := event.Data["save_path"].(string)
	finalPath, _ := event.Data["final_path"].(string)

	// Create the sync job
	newSyncJob, createErr := c.db.SyncJob.Create().
		SetDownloadJobID(dlJob.ID).
		SetRemoteBase(savePath).
		SetLocalBase(""). // Will be set by the syncer when sync starts
		SetStatus(syncjob.StatusPending).
		Save(ctx)
	if createErr != nil {
		c.logger.Error().Err(createErr).
			Str("download", dlJob.Name).
			Msg("failed to create sync job")
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("sync_job_id", newSyncJob.ID.String()).
		Msg("created sync job for download")

	// Emit sync job created event
	c.eventBus.Publish(events.Event{
		Type:    events.SyncJobCreated,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": newSyncJob.ID.String(),
			"final_path":  finalPath,
		},
	})
}

func (c *Controller) handleFileCompleted(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	// Get the sync job for this download
	sj, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		if generated.IsNotFound(err) {
			// This is expected when no enabled app exists for the download's category -
			// we intentionally don't create sync jobs for those downloads, so FileCompleted
			// events should be silently ignored.
			c.logger.Debug().
				Str("download", dlJob.Name).
				Msg("no sync job for download, skipping file completed event")
		} else {
			// Actual database error - log it
			c.logger.Error().Err(err).
				Str("download", dlJob.Name).
				Msg("failed to get sync job for download")
		}
		return
	}

	// Extract file data from event
	filePath, _ := event.Data["file_path"].(string)
	fileSize, _ := event.Data["file_size"].(int64)
	downloadFileIDStr, _ := event.Data["download_file_id"].(string)

	// Parse download file ID - required for linking SyncFile to DownloadFile
	if downloadFileIDStr == "" {
		c.logger.Error().
			Str("download", dlJob.Name).
			Str("file", filePath).
			Msg("missing download_file_id in file.completed event")
		return
	}
	downloadFileID, parseErr := ulid.Parse(downloadFileIDStr)
	if parseErr != nil {
		c.logger.Error().Err(parseErr).
			Str("download", dlJob.Name).
			Str("file", filePath).
			Str("download_file_id", downloadFileIDStr).
			Msg("invalid download_file_id in file.completed event")
		return
	}

	// Check if file record already exists
	existingFiles, err := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("sync_job_id", sj.ID.String()).
			Msg("failed to get sync files")
		return
	}

	// Look for existing file record
	var existingFile *generated.SyncFile
	for _, f := range existingFiles {
		if f.RelativePath == filePath {
			existingFile = f
			break
		}
	}

	if existingFile != nil {
		c.updateExistingFile(ctx, dlJob, sj.ID, existingFile)
		return
	}

	c.createNewSyncFile(ctx, dlJob, sj.ID, filePath, fileSize, downloadFileID)
}

func (c *Controller) updateExistingFile(
	_ context.Context,
	dlJob *generated.DownloadJob,
	syncJobID ulid.ULID,
	existingFile *generated.SyncFile,
) {
	// If the file is already syncing or complete, nothing to do
	if existingFile.Status != syncfile.StatusPending {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Str("file", existingFile.RelativePath).
			Str("status", string(existingFile.Status)).
			Msg("file already being synced or complete")
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("file", existingFile.RelativePath).
		Int64("size", existingFile.Size).
		Msg("file record exists, triggering sync")

	// Emit sync file created event - this triggers the transfer
	c.eventBus.Publish(events.Event{
		Type:    events.SyncFileCreated,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id":  syncJobID.String(),
			"sync_file_id": existingFile.ID.String(),
			"file_path":    existingFile.RelativePath,
			"file_size":    existingFile.Size,
		},
	})
}

func (c *Controller) createNewSyncFile(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	syncJobID ulid.ULID,
	filePath string,
	fileSize int64,
	downloadFileID ulid.ULID,
) {
	newSyncFile, err := c.db.SyncFile.Create().
		SetSyncJobID(syncJobID).
		SetDownloadFileID(downloadFileID).
		SetRelativePath(filePath).
		SetSize(fileSize).
		SetStatus(syncfile.StatusPending).
		Save(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("file", filePath).
			Msg("failed to create sync file")
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("file", filePath).
		Int64("size", fileSize).
		Msg("created sync file record")

	// Emit sync file created event - this triggers the transfer
	c.eventBus.Publish(events.Event{
		Type:    events.SyncFileCreated,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id":  syncJobID.String(),
			"sync_file_id": newSyncFile.ID.String(),
			"file_path":    filePath,
			"file_size":    fileSize,
		},
	})
}

// handleSyncFileCreated starts the file transfer when a sync file is created.
func (c *Controller) handleSyncFileCreated(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	if c.transferer == nil {
		c.logger.Warn().
			Str("download", dlJob.Name).
			Msg("no transfer backend configured, skipping file sync")
		return
	}

	syncJobIDStr, _ := event.Data["sync_job_id"].(string)
	syncFileIDStr, _ := event.Data["sync_file_id"].(string)
	filePath, _ := event.Data["file_path"].(string)
	fileSize, _ := event.Data["file_size"].(int64)

	syncJobID, _ := ulid.Parse(syncJobIDStr)
	syncFileID, _ := ulid.Parse(syncFileIDStr)

	// Get the sync job to get paths
	sj, err := c.db.SyncJob.Get(ctx, syncJobID)
	if err != nil {
		c.logger.Error().Err(err).
			Str("sync_job_id", syncJobIDStr).
			Msg("failed to get sync job")
		return
	}

	// Check if job is cancelled
	if sj.CancelledAt != nil {
		c.logger.Debug().
			Str("sync_job_id", syncJobIDStr).
			Msg("sync job is cancelled, skipping file transfer")
		return
	}

	// Check if file already exists before starting transfer
	if c.checkFileAlreadyExists(ctx, dlJob, sj, syncFileID, filePath, fileSize) {
		return
	}

	// Start the transfer in a goroutine
	c.wg.Add(1)
	go c.transferFile(ctx, dlJob, sj, syncFileID, filePath, fileSize)
}

// checkFileAlreadyExists checks if a file already exists with the correct size.
// It checks both the final destination and the staging directory.
// If found in either location, marks it complete and returns true to skip the transfer.
func (c *Controller) checkFileAlreadyExists(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
) bool {
	// First, check if file exists in the FINAL destination
	// This handles the case where files were previously synced but DB was reset
	if c.checkFileInFinalDestination(ctx, dlJob, sj, syncFileID, filePath, fileSize) {
		return true
	}

	// Then, check if file exists in STAGING directory
	// This handles resumed syncs where file is partially/fully transferred to staging
	return c.checkFileInStaging(ctx, dlJob, sj, syncFileID, filePath, fileSize)
}

// checkFileInFinalDestination checks if a file already exists in its final destination.
func (c *Controller) checkFileInFinalDestination(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
) bool {
	// Get the final destination path for this download
	finalPath := c.getFinalPathForDownload(ctx, dlJob)
	if finalPath == "" {
		return false
	}

	// Construct the full path: finalPath/filePath
	// (same as moveToFinal - does NOT include dlJob.Name separately)
	finalFilePath := filepath.Join(finalPath, filePath)

	// Check if file exists with correct size
	info, err := os.Stat(finalFilePath)
	if err != nil || info.Size() != fileSize {
		return false
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("file", filePath).
		Str("location", "final_destination").
		Int64("size", fileSize).
		Msg("file already exists in final destination, skipping transfer")

	return c.markFileAsAlreadySynced(ctx, dlJob, sj, syncFileID, filePath, fileSize)
}

// checkFileInStaging checks if a file already exists in the staging directory.
func (c *Controller) checkFileInStaging(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
) bool {
	// Calculate local path using the same logic as setupTransferPaths
	localBase := sj.LocalBase
	if localBase == "" {
		localBase = filepath.Join(c.syncingPath, fmt.Sprintf("job_%s", sj.ID.String()))
	}
	localPath := filepath.Join(localBase, filePath)

	// Check if file exists with correct size
	info, err := os.Stat(localPath)
	if err != nil || info.Size() != fileSize {
		return false
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("file", filePath).
		Str("location", "staging").
		Int64("size", fileSize).
		Msg("file already exists in staging, skipping transfer")

	return c.markFileAsAlreadySynced(ctx, dlJob, sj, syncFileID, filePath, fileSize)
}

// markFileAsAlreadySynced updates the file status to complete and emits events.
func (c *Controller) markFileAsAlreadySynced(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
) bool {
	// Update file status to complete
	if updateErr := c.updateSyncFileStatus(
		ctx,
		sj.ID,
		syncFileID,
		syncfile.StatusComplete,
		"",
	); updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("sync_file_id", syncFileID.String()).
			Msg("failed to update file status to complete")
		return false
	}

	// Update syncer status
	if c.syncer != nil {
		c.syncer.RegisterDownload(dlJob.DownloadClientID.String(), dlJob.RemoteID, dlJob.Name)
		c.syncer.RegisterFile(dlJob.DownloadClientID.String(), dlJob.RemoteID, filePath, fileSize)
		c.syncer.UpdateFileStatus(
			dlJob.DownloadClientID.String(),
			dlJob.RemoteID,
			filePath,
			FileStatusComplete,
		)
	}

	// Emit file complete event with already_synced flag
	c.eventBus.Publish(events.Event{
		Type:    events.SyncFileComplete,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id":    sj.ID.String(),
			"file_path":      filePath,
			"file_size":      fileSize,
			"already_synced": true,
		},
	})

	// Check if all files are complete
	c.checkSyncJobComplete(ctx, dlJob, sj)

	return true
}

// transferFile performs the actual file transfer.
func (c *Controller) transferFile(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
) {
	defer c.wg.Done()

	// Acquire semaphore for concurrency control
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return
	}

	// Set up local path in staging directory
	localBase, remotePath, localPath := c.setupTransferPaths(ctx, sj, filePath)

	c.logger.Debug().
		Str("download", dlJob.Name).
		Str("file", filePath).
		Str("remote", remotePath).
		Str("local", localPath).
		Int64("size", fileSize).
		Msg("starting file transfer")

	// Register with syncer and update statuses
	fileProgress := c.registerFileWithSyncer(dlJob, filePath, fileSize)
	if !c.markFileAsSyncing(ctx, dlJob, sj, syncFileID, filePath) {
		return
	}

	// Emit sync events
	c.emitSyncStartedIfNeeded(ctx, dlJob, sj)
	c.emitSyncFileStarted(dlJob, sj, filePath, fileSize)

	// Create local directory
	if err := os.MkdirAll(filepath.Dir(localPath), 0750); err != nil {
		dirErr := fmt.Errorf("failed to create directory: %w", err)
		c.handleTransferError(ctx, dlJob, sj, syncFileID, filePath, dirErr)
		return
	}

	// Check if file already exists with correct size (resume support)
	if info, err := os.Stat(localPath); err == nil && info.Size() == fileSize {
		c.logger.Info().
			Str("file", filePath).
			Int64("size", fileSize).
			Msg("file already exists with correct size, marking complete")
		c.handleTransferComplete(ctx, dlJob, sj, syncFileID, filePath, fileSize, true)
		return
	}

	// Execute the transfer
	if !c.executeTransfer(
		ctx,
		dlJob,
		sj,
		syncFileID,
		filePath,
		fileSize,
		localBase,
		remotePath,
		localPath,
		fileProgress,
	) {
		return
	}

	c.handleTransferComplete(ctx, dlJob, sj, syncFileID, filePath, fileSize, false)
}

// setupTransferPaths sets up the local and remote paths for a file transfer.
// Returns: localBase, remotePath, localPath.
func (c *Controller) setupTransferPaths(
	ctx context.Context,
	sj *generated.SyncJob,
	filePath string,
) (string, string, string) {
	localBase := sj.LocalBase
	if localBase == "" {
		localBase = filepath.Join(c.syncingPath, fmt.Sprintf("job_%s", sj.ID.String()))
		// Update the sync job with local base
		if _, updateErr := sj.Update().SetLocalBase(localBase).Save(ctx); updateErr != nil {
			c.logger.Error().Err(updateErr).
				Str("sync_job_id", sj.ID.String()).
				Msg("failed to update sync job local base")
		}
		sj.LocalBase = localBase
	}
	remotePath := filepath.Join(sj.RemoteBase, filePath)
	localPath := filepath.Join(localBase, filePath)
	return localBase, remotePath, localPath
}

// registerFileWithSyncer registers the file with the syncer for live progress tracking.
func (c *Controller) registerFileWithSyncer(
	dlJob *generated.DownloadJob,
	filePath string,
	fileSize int64,
) *FileProgress {
	if c.syncer == nil {
		return nil
	}
	c.syncer.RegisterDownload(dlJob.DownloadClientID.String(), dlJob.RemoteID, dlJob.Name)
	return c.syncer.RegisterFile(dlJob.DownloadClientID.String(), dlJob.RemoteID, filePath, fileSize)
}

// markFileAsSyncing updates the file status to syncing in DB and syncer.
func (c *Controller) markFileAsSyncing(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
) bool {
	if updateErr := c.updateSyncFileStatus(ctx, sj.ID, syncFileID, syncfile.StatusSyncing, ""); updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("sync_file_id", syncFileID.String()).
			Msg("failed to update file status to syncing")
		return false
	}
	if c.syncer != nil {
		c.syncer.UpdateFileStatus(
			dlJob.DownloadClientID.String(),
			dlJob.RemoteID,
			filePath,
			FileStatusSyncing,
		)
		c.syncer.MarkDownloadSyncing(dlJob.DownloadClientID.String(), dlJob.RemoteID)
	}
	return true
}

// executeTransfer performs the actual file transfer and verification.
func (c *Controller) executeTransfer(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
	_, remotePath, localPath string,
	fileProgress *FileProgress,
) bool {
	req := transfer.Request{
		RemotePath: remotePath,
		LocalPath:  localPath,
		Size:       fileSize,
	}

	transferErr := c.transferer.Transfer(ctx, req, func(p transfer.Progress) {
		if fileProgress != nil {
			fileProgress.SetProgress(p.Transferred, p.BytesPerSec)
		}
	})

	if transferErr != nil {
		if errors.Is(transferErr, context.Canceled) {
			c.logger.Debug().Str("file", filePath).Msg("transfer cancelled")
			return false
		}
		c.handleTransferError(ctx, dlJob, sj, syncFileID, filePath, transferErr)
		return false
	}

	// Verify transferred file
	info, err := os.Stat(localPath)
	if err != nil {
		c.handleTransferError(ctx, dlJob, sj, syncFileID, filePath,
			fmt.Errorf("file not found after transfer: %w", err))
		return false
	}
	if info.Size() != fileSize {
		c.handleTransferError(ctx, dlJob, sj, syncFileID, filePath,
			fmt.Errorf("size mismatch: expected %d, got %d", fileSize, info.Size()))
		return false
	}

	return true
}

func (c *Controller) updateSyncFileStatus(
	ctx context.Context,
	syncJobID, syncFileID ulid.ULID,
	status syncfile.Status,
	errMsg string,
) error {
	files, err := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(syncJobID)).
		All(ctx)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.ID == syncFileID {
			update := f.Update().
				SetStatus(status).
				SetErrorMessage(errMsg)
			if status == syncfile.StatusComplete {
				update.SetSyncedSize(f.Size)
			}
			_, saveErr := update.Save(ctx)
			return saveErr
		}
	}

	return fmt.Errorf("sync file %s not found in job %s", syncFileID.String(), syncJobID.String())
}

func (c *Controller) emitSyncStartedIfNeeded(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
) {
	// Check if this is the first file being synced (StartedAt is null until sync starts)
	if sj.StartedAt == nil {
		now := time.Now()
		_, updateErr := sj.Update().
			SetStartedAt(now).
			SetStatus(syncjob.StatusSyncing).
			Save(ctx)
		if updateErr != nil {
			c.logger.Error().Err(updateErr).
				Str("sync_job_id", sj.ID.String()).
				Msg("failed to update sync job start time")
		}
		sj.StartedAt = &now
		sj.Status = syncjob.StatusSyncing

		c.eventBus.Publish(events.Event{
			Type:    events.SyncStarted,
			Subject: dlJob,
			Data: map[string]any{
				"sync_job_id": sj.ID.String(),
				"local_base":  sj.LocalBase,
			},
		})
	}
}

func (c *Controller) emitSyncFileStarted(
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	filePath string,
	fileSize int64,
) {
	c.eventBus.Publish(events.Event{
		Type:    events.SyncFileStarted,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": sj.ID.String(),
			"file_path":   filePath,
			"file_size":   fileSize,
		},
	})
}

func (c *Controller) handleTransferError(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	err error,
) {
	c.logger.Error().Err(err).
		Str("download", dlJob.Name).
		Str("file", filePath).
		Msg("file transfer failed")

	_ = c.updateSyncFileStatus(ctx, sj.ID, syncFileID, syncfile.StatusError, err.Error())

	c.eventBus.Publish(events.Event{
		Type:    events.SyncFailed,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": sj.ID.String(),
			"file_path":   filePath,
			"error":       err.Error(),
		},
	})
}

func (c *Controller) handleTransferComplete(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
	syncFileID ulid.ULID,
	filePath string,
	fileSize int64,
	alreadySynced bool,
) {
	c.logger.Info().
		Str("download", dlJob.Name).
		Str("file", filePath).
		Int64("size", fileSize).
		Bool("already_synced", alreadySynced).
		Msg("file transfer complete")

	// Update file status
	if updateErr := c.updateSyncFileStatus(ctx, sj.ID, syncFileID, syncfile.StatusComplete, ""); updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("sync_file_id", syncFileID.String()).
			Msg("failed to update file status to complete")
	}

	// Update syncer status
	if c.syncer != nil {
		c.syncer.UpdateFileStatus(
			dlJob.DownloadClientID.String(),
			dlJob.RemoteID,
			filePath,
			FileStatusComplete,
		)
	}

	// Emit file complete event
	eventData := map[string]any{
		"sync_job_id": sj.ID.String(),
		"file_path":   filePath,
		"file_size":   fileSize,
	}
	if alreadySynced {
		eventData["already_synced"] = true
	}
	c.eventBus.Publish(events.Event{
		Type:    events.SyncFileComplete,
		Subject: dlJob,
		Data:    eventData,
	})

	// Check if all files are complete
	c.checkSyncJobComplete(ctx, dlJob, sj)
}

func (c *Controller) checkSyncJobComplete(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	sj *generated.SyncJob,
) {
	files, err := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("sync_job_id", sj.ID.String()).
			Msg("failed to get sync files for completion check")
		return
	}

	// Check if all files are complete
	allComplete := true
	for _, f := range files {
		if f.Status != syncfile.StatusComplete {
			allComplete = false
			break
		}
	}

	if !allComplete {
		return
	}

	// Mark sync job as complete
	now := time.Now()
	_, updateErr := sj.Update().
		SetStatus(syncjob.StatusComplete).
		SetCompletedAt(now).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("sync_job_id", sj.ID.String()).
			Msg("failed to mark sync job complete")
		return
	}
	sj.Status = syncjob.StatusComplete
	sj.CompletedAt = &now

	// Mark download complete in syncer and clean up
	if c.syncer != nil {
		c.syncer.MarkDownloadComplete(dlJob.DownloadClientID.String(), dlJob.RemoteID)
		c.syncer.RemoveByKey(dlJob.DownloadClientID.String(), dlJob.RemoteID)
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("sync_job_id", sj.ID.String()).
		Int("files", len(files)).
		Msg("sync job complete")

	// Get the final path from app configuration
	finalPath := c.getFinalPathForDownload(ctx, dlJob)

	// Emit sync complete event
	c.eventBus.Publish(events.Event{
		Type:    events.SyncComplete,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": sj.ID.String(),
			"local_base":  sj.LocalBase,
			"final_path":  finalPath,
		},
	})
}

// getFinalPathForDownload looks up the final destination path for a download based on its category.
// Priority: app.DownloadsPath > global downloadsPath/category.
func (c *Controller) getFinalPathForDownload(ctx context.Context, dlJob *generated.DownloadJob) string {
	if dlJob.Category == "" {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Msg("no category set on download, cannot determine final path")
		return ""
	}

	// Check if any app for this category has a custom downloads path
	apps, err := c.db.App.Query().
		Where(app.CategoryEQ(dlJob.Category)).
		All(ctx)
	if err == nil && len(apps) > 0 {
		if appPath := getDownloadsPathFromApps(apps); appPath != "" {
			return appPath
		}
	}

	// Fall back to global downloads path + downloader name + category
	if c.downloadsPath != "" {
		dlr, err := c.db.DownloadClient.Get(ctx, dlJob.DownloadClientID)
		if err != nil {
			c.logger.Error().Err(err).
				Str("download", dlJob.Name).
				Str("downloader_id", dlJob.DownloadClientID.String()).
				Msg("failed to get downloader for path computation")
			return ""
		}
		return filepath.Join(c.downloadsPath, dlr.Name, dlJob.Category)
	}

	c.logger.Debug().
		Str("download", dlJob.Name).
		Str("category", dlJob.Category).
		Msg("no downloads path configured (neither app nor global)")
	return ""
}

// syncCompletePaths holds the resolved paths for a sync complete operation.
type syncCompletePaths struct {
	syncJobID ulid.ULID
	localBase string
	finalPath string
}

// resolveSyncCompletePaths resolves paths from event data or database lookup.
func (c *Controller) resolveSyncCompletePaths(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	event events.Event,
) (*syncCompletePaths, error) {
	syncJobIDStr, _ := event.Data["sync_job_id"].(string)
	localBase, _ := event.Data["local_base"].(string)
	finalPath, _ := event.Data["final_path"].(string)

	syncJobID, _ := ulid.Parse(syncJobIDStr)

	// Look up paths from database if not provided in event data
	var zeroID ulid.ULID
	if localBase == "" || syncJobID == zeroID {
		sj, err := c.db.SyncJob.Query().
			Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get sync job: %w", err)
		}
		if syncJobID == zeroID {
			syncJobID = sj.ID
		}
		if localBase == "" {
			localBase = sj.LocalBase
		}
	}

	// Look up final path from app configuration if not provided
	if finalPath == "" {
		finalPath = c.getFinalPathForDownload(ctx, dlJob)
	}

	return &syncCompletePaths{
		syncJobID: syncJobID,
		localBase: localBase,
		finalPath: finalPath,
	}, nil
}

// handleSyncComplete moves synced files from staging to final destination.
func (c *Controller) handleSyncComplete(ctx context.Context, event events.Event) {
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	paths, err := c.resolveSyncCompletePaths(ctx, dlJob, event)
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("download", dlJob.Name).
			Msg("failed to resolve paths for move")
		return
	}

	if paths.finalPath == "" {
		c.logger.Warn().
			Str("download", dlJob.Name).
			Msg("no final path configured, skipping move")
		return
	}

	// If local_base is empty, files were already at final destination (DB was wiped but files remained)
	// In this case, skip the move and emit MoveComplete directly
	if paths.localBase == "" {
		c.logger.Info().
			Str("download", dlJob.Name).
			Str("final_path", paths.finalPath).
			Msg("files already at final destination, skipping move")
		c.emitMoveCompleteForExistingFiles(ctx, dlJob, paths)
		return
	}

	c.executeMoveJob(ctx, dlJob, paths)
}

// executeMoveJob creates and executes a move job for the given paths.
func (c *Controller) executeMoveJob(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	paths *syncCompletePaths,
) {
	mj, createErr := c.db.MoveJob.Create().
		SetDownloadJobID(dlJob.ID).
		SetSourcePath(paths.localBase).
		SetDestinationPath(filepath.Join(paths.finalPath, dlJob.Name)).
		SetStatus(movejob.StatusPending).
		Save(ctx)
	if createErr != nil {
		c.logger.Error().Err(createErr).Str("download", dlJob.Name).Msg("failed to create move job")
		return
	}

	// Update move job status to moving
	now := time.Now()
	_, updateErr := mj.Update().
		SetStatus(movejob.StatusMoving).
		SetStartedAt(now).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("move_job_id", mj.ID.String()).
			Msg("failed to update move job status")
	}
	mj.Status = movejob.StatusMoving
	mj.StartedAt = &now

	c.eventBus.Publish(events.Event{
		Type:    events.MoveStarted,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": paths.syncJobID.String(),
			"move_job_id": mj.ID.String(),
			"local_base":  paths.localBase,
			"final_path":  paths.finalPath,
		},
	})

	// Get sync job and execute move
	sj, err := c.db.SyncJob.Get(ctx, paths.syncJobID)
	if err != nil {
		c.logger.Error().Err(err).
			Str("sync_job_id", paths.syncJobID.String()).
			Msg("failed to get sync job for move")
		c.handleMoveError(ctx, dlJob, mj, paths.finalPath, err)
		return
	}

	if moveErr := c.moveToFinal(ctx, sj, paths.localBase, paths.finalPath); moveErr != nil {
		c.logger.Error().Err(moveErr).Str("download", dlJob.Name).Msg("failed to move files")
		c.handleMoveError(ctx, dlJob, mj, paths.finalPath, moveErr)
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("final_path", paths.finalPath).
		Msg("move complete")

	completedNow := time.Now()
	_, completeErr := mj.Update().
		SetStatus(movejob.StatusComplete).
		SetCompletedAt(completedNow).
		Save(ctx)
	if completeErr != nil {
		c.logger.Error().Err(completeErr).
			Str("move_job_id", mj.ID.String()).
			Msg("failed to update move job to complete")
	}

	c.eventBus.Publish(events.Event{
		Type:    events.MoveComplete,
		Subject: dlJob,
		Data: map[string]any{
			"sync_job_id": paths.syncJobID.String(),
			"move_job_id": mj.ID.String(),
			"final_path":  mj.DestinationPath,
		},
	})
}

// moveToFinal moves synced files from staging to final destination.
func (c *Controller) moveToFinal(
	ctx context.Context,
	sj *generated.SyncJob,
	localBase, finalPath string,
) error {
	c.logger.Info().
		Str("sync_job_id", sj.ID.String()).
		Str("from", localBase).
		Str("to", finalPath).
		Msg("moving to final destination")

	// Create final directory
	if err := os.MkdirAll(finalPath, 0750); err != nil {
		return fmt.Errorf("failed to create final directory: %w", err)
	}

	// Get sync files for this job
	files, err := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sync files: %w", err)
	}

	// Move each completed file
	for _, file := range files {
		if file.Status != syncfile.StatusComplete {
			continue
		}

		localFilePath := filepath.Join(localBase, file.RelativePath)
		finalFilePath := filepath.Join(finalPath, file.RelativePath)

		// Create parent directory
		if mkdirErr := os.MkdirAll(filepath.Dir(finalFilePath), 0750); mkdirErr != nil {
			return fmt.Errorf("failed to create directory for %s: %w", file.RelativePath, mkdirErr)
		}

		// Move file
		if renameErr := os.Rename(localFilePath, finalFilePath); renameErr != nil {
			// If rename fails (cross-device), try copy+delete
			if copyErr := fileutil.CopyFile(localFilePath, finalFilePath); copyErr != nil {
				return fmt.Errorf("failed to move file %s: %w", file.RelativePath, copyErr)
			}
			_ = os.Remove(localFilePath)
		}
	}

	// Clean up staging directory
	_ = os.RemoveAll(localBase)

	return nil
}

func (c *Controller) handleMoveError(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	mj *generated.MoveJob,
	finalPath string,
	err error,
) {
	// Update move job status to error
	_, updateErr := mj.Update().
		SetStatus(movejob.StatusError).
		SetErrorMessage(err.Error()).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("move_job_id", mj.ID.String()).
			Msg("failed to update move job error status")
	}

	// Publish move failed event
	c.eventBus.Publish(events.Event{
		Type:    events.MoveFailed,
		Subject: dlJob,
		Data: map[string]any{
			"move_job_id": mj.ID.String(),
			"final_path":  finalPath,
			"error":       err.Error(),
		},
	})
}

func (c *Controller) handleDownloadRemoved(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	// Clean up from syncer
	if c.syncer != nil {
		c.syncer.RemoveByKey(dlJob.DownloadClientID.String(), dlJob.RemoteID)
	}

	// Get the sync job
	sj, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Msg("no sync job found for removed download")
	}

	// Cancel and clean up staging if sync job exists
	if sj != nil {
		// Mark the sync job as cancelled
		now := time.Now()
		_, updateErr := sj.Update().
			SetStatus(syncjob.StatusCancelled).
			SetCancelledAt(now).
			Save(ctx)
		if updateErr != nil {
			c.logger.Error().Err(updateErr).
				Str("sync_job_id", sj.ID.String()).
				Msg("failed to mark sync job as cancelled")
		}

		// Clean up staging directory
		if sj.LocalBase != "" {
			if removeErr := os.RemoveAll(sj.LocalBase); removeErr != nil {
				c.logger.Warn().Err(removeErr).
					Str("path", sj.LocalBase).
					Msg("failed to cleanup staging directory")
			}
		}

		c.logger.Info().
			Str("download", dlJob.Name).
			Str("sync_job_id", sj.ID.String()).
			Msg("marked sync job as cancelled due to download removal")

		// Emit sync cancelled event
		c.eventBus.Publish(events.Event{
			Type:    events.SyncCancelled,
			Subject: dlJob,
			Data: map[string]any{
				"sync_job_id": sj.ID.String(),
				"reason":      "download_removed",
			},
		})
	}

	// Use PreviousCategory for cleanup (the category before any changes)
	// Fall back to current category if no previous category set
	categoryForCleanup := dlJob.PreviousCategory
	if categoryForCleanup == "" {
		categoryForCleanup = dlJob.Category
	}

	// Check if app wants cleanup on remove
	apps, err := c.db.App.Query().
		Where(app.CategoryEQ(categoryForCleanup)).
		All(ctx)
	if err != nil {
		c.logger.Debug().
			Str("category", categoryForCleanup).
			Msg("no apps found for category")
		return
	}

	shouldCleanup := false
	for _, a := range apps {
		if a.CleanupOnRemove {
			shouldCleanup = true
			break
		}
	}

	if shouldCleanup {
		c.cleanupFinalFiles(ctx, dlJob, apps)
	}
}

// cleanupFinalFiles removes files from the final destination.
func (c *Controller) cleanupFinalFiles(
	_ context.Context,
	dlJob *generated.DownloadJob,
	apps []*generated.App,
) {
	// Get the final path from app
	var finalPath string
	for _, a := range apps {
		if a.DownloadsPath != "" {
			finalPath = a.DownloadsPath
			break
		}
	}

	if finalPath == "" {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Msg("no final path configured, nothing to cleanup")
		return
	}

	// Construct path to the download's files
	downloadPath := filepath.Join(finalPath, dlJob.Name)

	// Check if path exists before attempting removal
	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Str("path", downloadPath).
			Msg("path does not exist, nothing to cleanup")
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("path", downloadPath).
		Msg("cleaning up files")

	// Remove the files
	if err := os.RemoveAll(downloadPath); err != nil {
		c.logger.Error().Err(err).
			Str("download", dlJob.Name).
			Str("path", downloadPath).
			Msg("failed to cleanup files")
		return
	}

	// Publish cleanup event
	c.eventBus.Publish(events.Event{
		Type:    events.Cleanup,
		Subject: dlJob,
		Data: map[string]any{
			"path": downloadPath,
		},
	})
}

func (c *Controller) handleCategoryChanged(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	// PreviousCategory contains the old category, Category contains the new one
	oldCategory := dlJob.PreviousCategory
	newCategory := dlJob.Category

	// Get apps for old and new categories
	oldApps, _ := c.db.App.Query().Where(app.CategoryEQ(oldCategory)).All(ctx)
	newApps, _ := c.db.App.Query().Where(app.CategoryEQ(newCategory)).All(ctx)

	// Handle cleanup for old category if app wants it
	shouldCleanup := c.handleCategoryCleanup(ctx, dlJob, oldApps)

	// Note: We do NOT update the download's category fields here.
	// The download model is owned by download.Controller, which already
	// updated the category in the database before publishing this event.

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("old_category", oldCategory).
		Str("new_category", newCategory).
		Bool("cleanup_performed", shouldCleanup).
		Msg("category change handled")

	// If no app for the new category, soft-delete sync job and files
	if len(newApps) == 0 {
		c.softDeleteSyncJobForDownload(ctx, dlJob)
		return
	}

	// Check if there's a soft-deleted sync job to reactivate
	c.reactivateSyncJobForDownload(ctx, dlJob)

	// Handle migration to new category (file operations only)
	c.handleCategoryMigration(ctx, dlJob, oldApps, newApps)
}

// handleCategoryCleanup cleans up files if the old category's app wants cleanup.
func (c *Controller) handleCategoryCleanup(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	oldApps []*generated.App,
) bool {
	for _, a := range oldApps {
		if a.CleanupOnCategoryChange {
			c.cleanupFinalFiles(ctx, dlJob, oldApps)
			return true
		}
	}
	return false
}

// handleCategoryMigration handles migrating files to a new category.
func (c *Controller) handleCategoryMigration(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	oldApps, newApps []*generated.App,
) {
	if len(newApps) == 0 {
		c.logger.Debug().
			Str("download", dlJob.Name).
			Msg("no apps for new category, skipping migration")
		return
	}

	// Get new final path from app
	newFinalPath := getDownloadsPathFromApps(newApps)
	if newFinalPath == "" {
		c.logger.Warn().
			Str("download", dlJob.Name).
			Msg("no download path for new category")
		return
	}

	// Note: Final path is now looked up dynamically from app configuration
	// when sync completes, so no need to update it here.

	// If files already exist at old location, migrate them
	oldPath := getDownloadsPathFromApps(oldApps)
	if oldPath != "" {
		fullOldPath := filepath.Join(oldPath, dlJob.Name)
		if _, statErr := os.Stat(fullOldPath); statErr == nil {
			c.migrateFilesForCategoryChange(ctx, dlJob, fullOldPath, newFinalPath)
		}
	}
}

// getDownloadsPathFromApps returns the first non-empty downloads path from apps.
func getDownloadsPathFromApps(apps []*generated.App) string {
	for _, a := range apps {
		if a.DownloadsPath != "" {
			return a.DownloadsPath
		}
	}
	return ""
}

// softDeleteSyncJobForDownload soft-deletes the sync job and its files for a download.
// This is used when a category changes to an untracked category.
func (c *Controller) softDeleteSyncJobForDownload(ctx context.Context, dlJob *generated.DownloadJob) {
	sj, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err != nil {
		// No sync job exists, nothing to delete
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("sync_job_id", sj.ID.String()).
		Msg("soft-deleting sync job for category change to untracked")

	// Delete all sync files first (they reference the sync job)
	_, deleteFilesErr := c.db.SyncFile.Delete().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		Exec(ctx)
	if deleteFilesErr != nil {
		c.logger.Error().Err(deleteFilesErr).
			Str("download", dlJob.Name).
			Msg("failed to soft-delete sync files")
	}

	// Delete the sync job
	deleteJobErr := c.db.SyncJob.DeleteOneID(sj.ID).Exec(ctx)
	if deleteJobErr != nil {
		c.logger.Error().Err(deleteJobErr).
			Str("download", dlJob.Name).
			Msg("failed to soft-delete sync job")
	}
}

// reactivateSyncJobForDownload reactivates a soft-deleted sync job and its files.
// This is used when a category changes back to a tracked category.
func (c *Controller) reactivateSyncJobForDownload(ctx context.Context, dlJob *generated.DownloadJob) {
	// First check if there's already an active sync job
	_, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(ctx)
	if err == nil {
		// Active sync job exists, nothing to reactivate
		return
	}

	// Query including soft-deleted records
	softDeleteCtx := mixins.SkipSoftDelete(ctx)
	sj, err := c.db.SyncJob.Query().
		Where(syncjob.DownloadJobIDEQ(dlJob.ID)).
		Only(softDeleteCtx)
	if err != nil {
		// No soft-deleted sync job found
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("sync_job_id", sj.ID.String()).
		Msg("reactivating soft-deleted sync job")

	// Reactivate the sync job by clearing deleted_at
	// Use UpdateOneID to avoid re-querying the record which would be filtered by soft-delete
	_, updateErr := c.db.SyncJob.UpdateOneID(sj.ID).
		ClearDeletedAt().
		Save(softDeleteCtx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("download", dlJob.Name).
			Msg("failed to reactivate sync job")
		return
	}

	// Reactivate all soft-deleted sync files for this sync job
	syncFiles, filesErr := c.db.SyncFile.Query().
		Where(syncfile.SyncJobIDEQ(sj.ID)).
		All(softDeleteCtx)
	if filesErr != nil {
		c.logger.Error().Err(filesErr).
			Str("download", dlJob.Name).
			Msg("failed to query soft-deleted sync files")
		return
	}

	for _, sf := range syncFiles {
		// Use UpdateOneID to avoid re-querying the record which would be filtered by soft-delete
		_, sfUpdateErr := c.db.SyncFile.UpdateOneID(sf.ID).
			ClearDeletedAt().
			Save(softDeleteCtx)
		if sfUpdateErr != nil {
			c.logger.Error().Err(sfUpdateErr).
				Str("download", dlJob.Name).
				Str("file", sf.RelativePath).
				Msg("failed to reactivate sync file")
		}
	}
}

// emitMoveCompleteForExistingFiles emits MoveComplete when files already exist at final destination.
// This handles the case where the DB was wiped but files were previously synced and moved.
func (c *Controller) emitMoveCompleteForExistingFiles(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	paths *syncCompletePaths,
) {
	finalDestination := filepath.Join(paths.finalPath, dlJob.Name)

	// Create a move job record for consistency (even though no actual move happened)
	mj, createErr := c.db.MoveJob.Create().
		SetDownloadJobID(dlJob.ID).
		SetSourcePath("").                    // No source - files already at destination
		SetDestinationPath(finalDestination). // Where files exist
		SetStatus(movejob.StatusComplete).    // Already complete
		SetStartedAt(time.Now()).
		SetCompletedAt(time.Now()).
		Save(ctx)
	if createErr != nil {
		c.logger.Error().Err(createErr).
			Str("download", dlJob.Name).
			Msg("failed to create move job record for existing files")
		// Continue anyway - the important part is emitting the event
	}

	eventData := map[string]any{
		"sync_job_id":      paths.syncJobID.String(),
		"final_path":       finalDestination,
		"already_at_final": true,
	}
	if mj != nil {
		eventData["move_job_id"] = mj.ID.String()
	}

	c.eventBus.Publish(events.Event{
		Type:    events.MoveComplete,
		Subject: dlJob,
		Data:    eventData,
	})
}

// migrateFilesForCategoryChange moves files from old app location to new app location.
// It only handles file operations and emits MoveComplete event.
// The app.Controller will respond to MoveComplete to handle app notifications.
func (c *Controller) migrateFilesForCategoryChange(
	_ context.Context,
	dlJob *generated.DownloadJob,
	oldPath, newBasePath string,
) {
	newPath := filepath.Join(newBasePath, dlJob.Name)

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("from", oldPath).
		Str("to", newPath).
		Msg("migrating files for category change")

	// Publish move started event
	c.eventBus.Publish(events.Event{
		Type:    events.MoveStarted,
		Subject: dlJob,
		Data: map[string]any{
			"from":       oldPath,
			"to":         newPath,
			"is_migrate": true,
		},
	})

	// Create destination directory
	if err := os.MkdirAll(newBasePath, 0750); err != nil {
		c.logger.Error().Err(err).
			Str("path", newBasePath).
			Msg("failed to create destination directory")

		c.eventBus.Publish(events.Event{
			Type:    events.MoveFailed,
			Subject: dlJob,
			Data: map[string]any{
				"from":       oldPath,
				"to":         newPath,
				"is_migrate": true,
				"error":      err.Error(),
			},
		})
		return
	}

	// Move files
	if err := os.Rename(oldPath, newPath); err != nil {
		// Cross-device rename, use copy+delete
		if copyErr := fileutil.CopyDir(oldPath, newPath); copyErr != nil {
			c.logger.Error().Err(copyErr).
				Str("download", dlJob.Name).
				Msg("failed to migrate files")

			c.eventBus.Publish(events.Event{
				Type:    events.MoveFailed,
				Subject: dlJob,
				Data: map[string]any{
					"from":       oldPath,
					"to":         newPath,
					"is_migrate": true,
					"error":      copyErr.Error(),
				},
			})
			return
		}
		_ = os.RemoveAll(oldPath)
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("new_path", newPath).
		Msg("file migration complete")

	// Publish move complete event - app.Controller will handle app notifications
	c.eventBus.Publish(events.Event{
		Type:    events.MoveComplete,
		Subject: dlJob,
		Data: map[string]any{
			"final_path": newPath,
			"is_migrate": true,
		},
	})
}
