package download

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadclient"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadfile"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadjob"
	"github.com/seedreap/seedreap/internal/ent/generated/syncjob"
	"github.com/seedreap/seedreap/internal/events"
)

// Default configuration values.
const (
	defaultPollInterval = 30 * time.Second
)

// downloadClientEntry holds both the database entity and API client for a download client.
type downloadClientEntry struct {
	entity *generated.DownloadClient
	client Client
}

// Controller coordinates all download-related operations including polling
// download clients, persisting discoveries, and publishing events.
type Controller struct {
	clients           map[ulid.ULID]downloadClientEntry
	eventBus          *events.Bus
	db                *generated.Client
	interval          time.Duration
	baseDownloadsPath string
	logger            zerolog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ControllerOption is a functional option for configuring the Controller.
type ControllerOption func(*Controller)

// WithControllerLogger sets the logger for the controller.
func WithControllerLogger(logger zerolog.Logger) ControllerOption {
	return func(c *Controller) {
		c.logger = logger
	}
}

// WithPollInterval sets the poll interval.
func WithPollInterval(d time.Duration) ControllerOption {
	return func(c *Controller) {
		c.interval = d
	}
}

// WithBaseDownloadsPath sets the base path for computing default download paths.
// When an app doesn't have an explicit downloads path, the default is computed as
// $baseDownloadsPath/$downloaderName/$category.
func WithBaseDownloadsPath(path string) ControllerOption {
	return func(c *Controller) {
		c.baseDownloadsPath = path
	}
}

// NewController creates a new download Controller and initializes its downloaders.
func NewController(
	eventBus *events.Bus,
	db *generated.Client,
	opts ...ControllerOption,
) *Controller {
	c := &Controller{
		clients:  make(map[ulid.ULID]downloadClientEntry),
		eventBus: eventBus,
		db:       db,
		interval: defaultPollInterval,
		logger:   zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Initialize download clients from configs
	c.initClients()

	return c
}

// initClients creates download client instances from configurations stored in the database.
func (c *Controller) initClients() {
	ctx := context.Background()
	clients, err := c.db.DownloadClient.Query().
		Where(downloadclient.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list download clients from database")
		return
	}

	for _, d := range clients {
		c.logger.Debug().Str("name", d.Name).Str("type", d.Type).Msg("configuring download client")

		switch d.Type {
		case "qbittorrent":
			client := NewQBittorrent(
				d,
				WithLogger(c.logger.With().Str("download_client", d.Name).Logger()),
			)
			c.clients[d.ID] = downloadClientEntry{
				entity: d,
				client: client,
			}

		default:
			c.logger.Warn().Str("type", d.Type).Msg("unknown download client type")
		}
	}
}

// GetDownloadClient returns a download client by ID.
// This is used internally by handlers that need to query download clients.
func (c *Controller) GetDownloadClient(id ulid.ULID) (Client, bool) {
	entry, ok := c.clients[id]
	if !ok {
		return nil, false
	}
	return entry.client, true
}

// SSHConfig returns the SSH configuration from the first configured download client.
// Returns an empty config if no download clients are configured.
func (c *Controller) SSHConfig() config.SSHConfig {
	ctx := context.Background()
	clients, err := c.db.DownloadClient.Query().
		Where(downloadclient.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list download clients for SSH config")
		return config.SSHConfig{}
	}

	for _, d := range clients {
		if d.SSHHost != "" {
			return config.SSHConfig{
				Host:           d.SSHHost,
				Port:           d.SSHPort,
				User:           d.SSHUser,
				KeyFile:        d.SSHKeyFile,
				KnownHostsFile: d.SSHKnownHostsFile,
				IgnoreHostKey:  d.SSHIgnoreHostKey,
				Timeout:        time.Duration(d.SSHTimeout) * time.Second,
			}
		}
	}
	return config.SSHConfig{}
}

// Start begins the controller's polling loop.
func (c *Controller) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Restore active downloads from store (for persistent database scenarios)
	c.restoreActiveDownloads(ctx)

	// Initial poll
	c.poll(ctx)

	// Start polling loop in background
	c.wg.Add(1)
	go c.pollLoop(ctx)

	c.logger.Info().
		Dur("interval", c.interval).
		Int("download_clients", len(c.clients)).
		Msg("download controller started")

	return nil
}

// Stop stops the controller and waits for the polling loop to finish.
func (c *Controller) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	c.logger.Info().Msg("download controller stopped")
	return nil
}

func (c *Controller) pollLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Controller) poll(ctx context.Context) {
	c.logger.Debug().Msg("polling download clients")

	for _, entry := range c.clients {
		dlcEntity := entry.entity
		dlcClient := entry.client

		c.logger.Debug().
			Str("download_client", dlcEntity.Name).
			Msg("listing downloads")

		// List all downloads from the client (no category filter)
		// We track all downloads to detect category changes
		apiDownloads, err := dlcClient.ListDownloads(ctx, nil)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("download_client", dlcEntity.Name).
				Msg("failed to list downloads")
			continue
		}

		// Load downloads from database
		downloadJobs, err := c.db.DownloadJob.Query().
			Where(downloadjob.DownloadClientIDEQ(dlcEntity.ID)).
			All(ctx)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("download_client", dlcEntity.Name).
				Msg("failed to list downloads from database")
			continue
		}

		jobMap := make(map[string]*generated.DownloadJob)
		for _, d := range downloadJobs {
			jobMap[d.RemoteID] = d
		}

		c.logger.Debug().
			Str("download_client", dlcEntity.Name).
			Int("count", len(apiDownloads)).
			Msg("found downloads")

		for _, apiInfo := range apiDownloads {
			dlJob := jobMap[apiInfo.ID]

			if dlJob == nil {
				c.handleNewDownload(ctx, dlcEntity, dlcClient, apiInfo)
			} else {
				c.updateDownload(ctx, dlcEntity, dlcClient, dlJob, apiInfo)
			}

			// remove from dbDownloadMap to track which downloads are no longer on the downloader
			delete(jobMap, apiInfo.ID)
		}

		// process downloads that were in the database but not returned by the download client
		for _, dlJob := range jobMap {
			c.logger.Info().
				Str("download", dlJob.Name).
				Str("download_client", dlcEntity.Name).
				Str("download_status", string(dlJob.Status)).
				Msg("download removed from download client")

			err = c.db.DownloadJob.DeleteOneID(dlJob.ID).Exec(ctx)
			if err != nil {
				c.logger.Error().Err(err).Msg("failed to remove old download job from database")
				continue
			}

			c.publishRemoved(dlJob)
		}
	}
}

// handleNewDownload processes a newly discovered download.
func (c *Controller) handleNewDownload(
	ctx context.Context,
	dlcEntity *generated.DownloadClient,
	dlcClient Client,
	apiInfo *Download,
) {
	// // Check if it has a matching app
	// apps, err := c.db.App.Query().
	// 	Where(app.CategoryEQ(apiInfo.Category)).
	// 	All(ctx)
	// if err != nil {
	// 	c.logger.Error().Err(err).
	// 		Str("download", apiInfo.Name).
	// 		Str("category", apiInfo.Category).
	// 		Msg("failed to list apps for category")
	// 	return
	// }
	// if len(apps) == 0 {
	// 	c.logger.Debug().
	// 		Str("download", apiInfo.Name).
	// 		Str("category", apiInfo.Category).
	// 		Msg("no apps for category, skipping")
	// 	return
	// }

	// Fetch file list for the discovery event
	files, filesErr := dlcClient.GetFiles(ctx, apiInfo.ID)
	if filesErr != nil {
		c.logger.Warn().
			Err(filesErr).
			Str("download", apiInfo.Name).
			Msg("failed to get files on discovery")
	} else {
		apiInfo.Files = files
	}

	// Map download client state
	downloadState := mapDownloadState(apiInfo.State)

	// Persist to database FIRST, before publishing event
	create := c.db.DownloadJob.Create().
		SetRemoteID(apiInfo.ID).
		SetDownloadClientID(dlcEntity.ID).
		SetName(apiInfo.Name).
		SetCategory(apiInfo.Category).
		SetStatus(downloadState).
		SetSize(apiInfo.Size).
		SetDownloaded(apiInfo.Downloaded).
		SetProgress(apiInfo.Progress).
		SetSavePath(apiInfo.SavePath).
		SetContentPath(apiInfo.ContentPath).
		SetDiscoveredAt(time.Now())

	// Set downloaded timestamp if already complete
	if downloadState == downloadjob.StatusComplete {
		create.SetDownloadedAt(time.Now())
	}

	newJob, err := create.Save(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("download", apiInfo.Name).
			Str("download_id", apiInfo.ID).
			Msg("failed to persist discovered download")
		return
	}

	c.logger.Info().
		Str("download", apiInfo.Name).
		Str("download_id", apiInfo.ID).
		Str("category", apiInfo.Category).
		Str("download_client", dlcEntity.Name).
		Str("download_state", string(downloadState)).
		Int("files", len(apiInfo.Files)).
		Msg("discovered and persisted new download")

	// Create DownloadFile records for each file
	c.createDownloadFiles(ctx, newJob, apiInfo.Files)

	// Publish discovery event
	c.publishDiscovered(newJob, apiInfo)

	// If already complete, also publish download complete event
	if downloadState == downloadjob.StatusComplete {
		c.publishDownloadComplete(newJob)
	}

	// Publish FileCompleted events for any files already complete
	c.publishFileCompletions(newJob, apiInfo)
}

// updateDownload processes updates to an existing download job.
func (c *Controller) updateDownload(
	ctx context.Context,
	_ *generated.DownloadClient,
	dlcClient Client,
	dlJob *generated.DownloadJob,
	apiInfo *Download,
) {
	logger := c.logger.With().
		Str("download", dlJob.Name).
		Str("download_hash", dlJob.RemoteID).
		Logger()

	previousStatus := dlJob.Status

	// Track changes we need to update in the database
	changes := map[string]any{}

	newStatus := mapDownloadState(apiInfo.State)
	if dlJob.Category != apiInfo.Category {
		changes["category"] = apiInfo.Category
		dlJob.PreviousCategory = dlJob.Category
		dlJob.Category = apiInfo.Category
	}

	if dlJob.Status != newStatus {
		changes["status"] = newStatus
		dlJob.Status = newStatus
	}

	// if the status is already complete and the status or category hasn't changed, skip further updates
	// can't think of a scenario where other fields would change after download completion so seems like a safe fast path
	if len(changes) == 0 && dlJob.Status == downloadjob.StatusComplete {
		logger.Debug().Msg("download complete and category unchanged, skipping update")
		return
	}

	if dlJob.Progress != apiInfo.Progress {
		changes["progress"] = apiInfo.Progress
		dlJob.Progress = apiInfo.Progress
	}

	if dlJob.Downloaded != apiInfo.Downloaded {
		changes["downloaded"] = apiInfo.Downloaded
		dlJob.Downloaded = apiInfo.Downloaded
	}

	if dlJob.Size != apiInfo.Size {
		changes["size"] = apiInfo.Size
		dlJob.Size = apiInfo.Size
	}

	if dlJob.ContentPath != apiInfo.ContentPath {
		changes["content_path"] = apiInfo.ContentPath
		dlJob.ContentPath = apiInfo.ContentPath
	}

	// Check for individual file completions (for incremental sync)
	files, err := dlcClient.GetFiles(ctx, apiInfo.ID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get files for download update")

		return
	}

	apiInfo.Files = files

	// Update download file records with latest progress
	c.updateDownloadFiles(ctx, dlJob, files)

	// Publish FileCompleted events for any newly completed files
	c.publishFileCompletions(dlJob, apiInfo)

	// Update database if needed
	if len(changes) == 0 {
		logger.Debug().Msg("no changes to download, skipping update")
		return
	}

	if err := c.persistDownloadChanges(ctx, dlJob, apiInfo, changes); err != nil {
		logger.Error().Err(err).Msg("failed to update download")
		return
	}

	// Publish that the download was updated
	c.publishDownloadUpdated(dlJob)

	if changes["category"] != nil {
		c.publishCategoryChanged(dlJob)
	}

	// Publish state change events
	c.publishStatusChange(logger, previousStatus, dlJob.Status, dlJob)
}

// persistDownloadChanges saves the tracked changes to the database.
func (c *Controller) persistDownloadChanges(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	apiInfo *Download,
	changes map[string]any,
) error {
	update := dlJob.Update()
	if changes["category"] != nil {
		update.SetCategory(apiInfo.Category).SetPreviousCategory(dlJob.PreviousCategory)
	}
	if changes["status"] != nil {
		update.SetStatus(dlJob.Status)
	}
	if changes["progress"] != nil {
		update.SetProgress(apiInfo.Progress)
	}
	if changes["downloaded"] != nil {
		update.SetDownloaded(apiInfo.Downloaded)
	}
	if changes["size"] != nil {
		update.SetSize(apiInfo.Size)
	}
	if changes["content_path"] != nil {
		update.SetContentPath(apiInfo.ContentPath)
	}

	_, err := update.Save(ctx)
	return err
}

func (c *Controller) publishStatusChange(
	logger zerolog.Logger,
	prevStatus, newStatus downloadjob.Status,
	dlJob *generated.DownloadJob,
) {
	if prevStatus == newStatus {
		return
	}

	logger.Info().
		Str("old_status", string(prevStatus)).
		Str("new_status", string(newStatus)).
		Msg("download status transitioned")

	switch {
	case newStatus == downloadjob.StatusPaused:
		c.publishDownloadPaused(dlJob)
	case prevStatus == downloadjob.StatusPaused &&
		newStatus == downloadjob.StatusDownloading:
		c.publishDownloadResumed(dlJob)
	case newStatus == downloadjob.StatusComplete:
		now := time.Now()
		dlJob.DownloadedAt = &now
		c.publishDownloadComplete(dlJob)
	case newStatus == downloadjob.StatusError:
		c.publishDownloadError(dlJob)
	}
}

// createDownloadFiles creates DownloadFile records for all files in a download.
func (c *Controller) createDownloadFiles(ctx context.Context, dlJob *generated.DownloadJob, files []File) {
	for i := range files {
		f := &files[i]
		progress := calculateProgress(f.Downloaded, f.Size)
		_, err := c.db.DownloadFile.Create().
			SetDownloadJobID(dlJob.ID).
			SetRelativePath(f.Path).
			SetSize(f.Size).
			SetDownloaded(f.Downloaded).
			SetProgress(progress).
			SetPriority(f.Priority).
			Save(ctx)
		if err != nil {
			c.logger.Error().Err(err).
				Str("download", dlJob.Name).
				Str("file", f.Path).
				Msg("failed to create download file record")
		}
	}
}

// calculateProgress computes progress as a ratio from 0.0 to 1.0.
func calculateProgress(downloaded, size int64) float64 {
	if size <= 0 {
		return 0
	}
	return float64(downloaded) / float64(size)
}

// updateDownloadFiles updates DownloadFile records with latest progress from the downloader.
func (c *Controller) updateDownloadFiles(ctx context.Context, dlJob *generated.DownloadJob, files []File) {
	// Get existing download files from store
	existingFiles, err := c.db.DownloadFile.Query().
		Where(downloadfile.DownloadJobIDEQ(dlJob.ID)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("download", dlJob.Name).
			Msg("failed to get download files for update")
		return
	}

	// Build map of path -> DownloadFile for quick lookup
	fileMap := make(map[string]*generated.DownloadFile)
	for _, df := range existingFiles {
		fileMap[df.RelativePath] = df
	}

	for i := range files {
		f := &files[i]
		existingFile := fileMap[f.Path]
		progress := calculateProgress(f.Downloaded, f.Size)

		if existingFile == nil {
			// New file - create it
			_, createErr := c.db.DownloadFile.Create().
				SetDownloadJobID(dlJob.ID).
				SetRelativePath(f.Path).
				SetSize(f.Size).
				SetDownloaded(f.Downloaded).
				SetProgress(progress).
				SetPriority(f.Priority).
				Save(ctx)
			if createErr != nil {
				c.logger.Error().Err(createErr).
					Str("download", dlJob.Name).
					Str("file", f.Path).
					Msg("failed to create new download file record")
			}
			continue
		}

		// Check if anything changed
		if existingFile.Downloaded == f.Downloaded &&
			existingFile.Size == f.Size &&
			existingFile.Priority == f.Priority {
			continue
		}

		// Update the file
		_, updateErr := existingFile.Update().
			SetDownloaded(f.Downloaded).
			SetProgress(progress).
			SetSize(f.Size).
			SetPriority(f.Priority).
			Save(ctx)
		if updateErr != nil {
			c.logger.Error().Err(updateErr).
				Str("download", dlJob.Name).
				Str("file", f.Path).
				Msg("failed to update download file record")
		}
	}
}

// publishFileCompletions publishes FileCompleted events for all complete files in a download.
// Deduplication is handled by the receiver (filesync controller).
func (c *Controller) publishFileCompletions(dlJob *generated.DownloadJob, apiInfo *Download) {
	pendingCount := 0
	for _, f := range apiInfo.Files {
		if f.Priority > 0 && f.State != FileStateComplete {
			pendingCount++
		}
	}

	// Get download files from store to include their IDs in events
	ctx := context.Background()
	downloadFiles, err := c.db.DownloadFile.Query().
		Where(downloadfile.DownloadJobIDEQ(dlJob.ID)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("download", dlJob.Name).
			Msg("failed to get download files for completion events")
		return
	}

	// Build map of path -> DownloadFile for quick lookup
	fileMap := make(map[string]*generated.DownloadFile)
	for _, df := range downloadFiles {
		fileMap[df.RelativePath] = df
	}

	for i := range apiInfo.Files {
		f := &apiInfo.Files[i]
		// Skip files not selected for download
		if f.Priority == 0 {
			continue
		}
		// Only publish for complete files
		if f.State != FileStateComplete {
			continue
		}

		// Find the corresponding DownloadFile record
		downloadFile := fileMap[f.Path]
		c.publishFileCompleted(dlJob, f, downloadFile, pendingCount)
	}
}

// mapDownloadState converts download.TorrentState to downloadjob.Status.
func mapDownloadState(state TorrentState) downloadjob.Status {
	switch state {
	case TorrentStateComplete:
		return downloadjob.StatusComplete
	case TorrentStatePaused:
		return downloadjob.StatusPaused
	case TorrentStateError:
		return downloadjob.StatusError
	default:
		return downloadjob.StatusDownloading
	}
}

func (c *Controller) publishDiscovered(
	dl *generated.DownloadJob,
	apiDL *Download,
) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadDiscovered,
		Subject: dl,
		Data: map[string]any{
			"save_path":    dl.SavePath,
			"content_path": dl.ContentPath,
			"file_count":   len(apiDL.Files),
		},
	})
}

func (c *Controller) publishCategoryChanged(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.CategoryChanged,
		Subject: d,
	})
}

func (c *Controller) publishDownloadComplete(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadComplete,
		Subject: d,
	})
}

func (c *Controller) publishRemoved(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadRemoved,
		Subject: d,
	})
}

func (c *Controller) publishDownloadPaused(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadPaused,
		Subject: d,
	})
}

func (c *Controller) publishDownloadResumed(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadResumed,
		Subject: d,
	})
}

func (c *Controller) publishDownloadUpdated(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadUpdated,
		Subject: d,
	})
}

func (c *Controller) publishDownloadError(d *generated.DownloadJob) {
	c.eventBus.Publish(events.Event{
		Type:    events.DownloadError,
		Subject: d,
	})
}

func (c *Controller) publishFileCompleted(
	dlJob *generated.DownloadJob,
	f *File,
	downloadFile *generated.DownloadFile,
	pendingCount int,
) {
	data := map[string]any{
		"file_path":     f.Path,
		"file_size":     f.Size,
		"files_pending": pendingCount,
	}
	// Include the download file ID if we have it
	if downloadFile != nil {
		data["download_file_id"] = downloadFile.ID.String()
	}
	c.eventBus.Publish(events.Event{
		Type:    events.FileCompleted,
		Subject: dlJob,
		Data:    data,
	})
}

// TriggerPoll triggers an immediate poll cycle. Useful for testing.
func (c *Controller) TriggerPoll(ctx context.Context) {
	c.poll(ctx)
}

// restoreActiveDownloads re-emits discovery events for downloads that have incomplete
// sync jobs. This ensures the syncer and other handlers get re-initialized with file
// details when using a persistent database.
func (c *Controller) restoreActiveDownloads(ctx context.Context) {
	// Get sync jobs that are still active (pending or syncing)
	syncJobs, err := c.db.SyncJob.Query().
		Where(syncjob.StatusIn(syncjob.StatusPending, syncjob.StatusSyncing)).
		WithDownloadJob().
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to list active sync jobs for restore")
		return
	}

	if len(syncJobs) == 0 {
		return
	}

	c.logger.Info().Int("count", len(syncJobs)).Msg("restoring downloads with active sync jobs")

	for _, sj := range syncJobs {
		// Get the download job (eagerly loaded)
		dlJob := sj.Edges.DownloadJob
		if dlJob == nil {
			c.logger.Warn().
				Str("sync_job_id", sj.ID.String()).
				Msg("download job not found for sync job, skipping")
			continue
		}

		// Get the download client
		dlcEntry, ok := c.clients[dlJob.DownloadClientID]
		if !ok {
			c.logger.Warn().
				Str("download", dlJob.Name).
				Str("download_client_id", dlJob.DownloadClientID.String()).
				Msg("download client not found, skipping restore")
			continue
		}
		dlcName := dlcEntry.entity.Name

		// Fetch current download info from download client
		dlInfo, dlErr := dlcEntry.client.GetDownload(ctx, dlJob.RemoteID)
		if dlErr != nil {
			c.logger.Warn().Err(dlErr).
				Str("download", dlJob.Name).
				Str("download_client", dlcName).
				Msg("download no longer exists in download client, will be cleaned up on next poll")
			continue
		}

		// Fetch file details
		files, filesErr := dlcEntry.client.GetFiles(ctx, dlJob.RemoteID)
		if filesErr != nil {
			c.logger.Warn().Err(filesErr).
				Str("download", dlJob.Name).
				Str("download_client", dlcName).
				Msg("failed to get files for restore")
		} else {
			dlInfo.Files = files
		}

		c.logger.Info().
			Str("download", dlJob.Name).
			Str("download_client", dlcName).
			Str("sync_status", string(sj.Status)).
			Int("files", len(dlInfo.Files)).
			Msg("restoring download")

		// Re-emit discovery event to re-initialize handlers
		c.publishDiscovered(dlJob, dlInfo)
	}
}
