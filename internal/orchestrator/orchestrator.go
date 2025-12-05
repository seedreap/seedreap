// Package orchestrator coordinates syncing between downloaders and apps.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/fileutil"
	"github.com/seedreap/seedreap/internal/timeline"
)

// DownloadState tracks the state of a download through the sync pipeline.
type DownloadState string

// Default configuration values.
const (
	defaultPollInterval    = 30 * time.Second
	defaultShutdownTimeout = 10 * time.Second
)

const (
	// StateDiscovered indicates the download was just discovered.
	StateDiscovered DownloadState = "discovered"
	// StateSyncing indicates files are being synced.
	StateSyncing DownloadState = "syncing"
	// StateSynced indicates all files have been synced.
	StateSynced DownloadState = "synced"
	// StateMoving indicates files are being moved to final location.
	StateMoving DownloadState = "moving"
	// StateImporting indicates the app is importing.
	StateImporting DownloadState = "importing"
	// StateComplete indicates the download has been fully processed.
	StateComplete DownloadState = "complete"
	// StateError indicates an error occurred.
	StateError DownloadState = "error"
)

// TrackedDownload represents a download being tracked by the orchestrator.
type TrackedDownload struct {
	Download         *download.Download
	DownloaderName   string
	OriginalCategory string // Category when first discovered, used to detect post-import category changes
	State            DownloadState
	SyncDownload     *filesync.SyncDownload
	Apps             []app.App
	Error            error
	DiscoveredAt     time.Time
	CompletedAt      time.Time
	mu               sync.RWMutex
}

// GetState returns the current state thread-safely.
func (td *TrackedDownload) GetState() DownloadState {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.State
}

// GetDownload returns the download info thread-safely.
func (td *TrackedDownload) GetDownload() *download.Download {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.Download
}

// GetError returns the error if any.
func (td *TrackedDownload) GetError() error {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.Error
}

// GetTimes returns the discovered and completed times.
//
//nolint:nonamedreturns // named returns document multiple time.Time values
func (td *TrackedDownload) GetTimes() (discoveredAt, completedAt time.Time) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.DiscoveredAt, td.CompletedAt
}

// GetSyncDownload returns the sync download.
func (td *TrackedDownload) GetSyncDownload() *filesync.SyncDownload {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.SyncDownload
}

// Orchestrator coordinates the sync pipeline.
type Orchestrator struct {
	downloaders   *download.Registry
	apps          *app.Registry
	syncer        *filesync.Syncer
	timeline      timeline.Recorder
	pollInterval  time.Duration
	downloadsPath string
	logger        zerolog.Logger

	tracked   map[string]*TrackedDownload // key: downloaderName:downloadID
	trackedMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Option is a functional option for configuring the orchestrator.
type Option func(*Orchestrator)

// WithLogger sets the logger.
func WithLogger(logger zerolog.Logger) Option {
	return func(o *Orchestrator) {
		o.logger = logger
	}
}

// WithPollInterval sets the poll interval.
func WithPollInterval(d time.Duration) Option {
	return func(o *Orchestrator) {
		o.pollInterval = d
	}
}

// WithTimeline sets the timeline recorder.
func WithTimeline(t timeline.Recorder) Option {
	return func(o *Orchestrator) {
		o.timeline = t
	}
}

// New creates a new Orchestrator.
func New(
	downloaders *download.Registry,
	apps *app.Registry,
	syncr *filesync.Syncer,
	downloadsPath string,
	opts ...Option,
) *Orchestrator {
	o := &Orchestrator{
		downloaders:   downloaders,
		apps:          apps,
		syncer:        syncr,
		downloadsPath: downloadsPath,
		pollInterval:  defaultPollInterval,
		logger:        zerolog.Nop(),
		tracked:       make(map[string]*TrackedDownload),
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// Start begins the orchestration loop.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.ctx, o.cancel = context.WithCancel(ctx)

	// Record system start
	o.recordEvent(timeline.EventSystemStarted, "System started", "", "", "", "", map[string]any{
		"downloaders": len(o.downloaders.All()),
		"apps":        len(o.apps.All()),
	})

	// Connect to all downloaders
	for name, dl := range o.downloaders.All() {
		if err := dl.Connect(o.ctx); err != nil {
			return fmt.Errorf("failed to connect to downloader %s: %w", name, err)
		}
		o.recordEvent(
			timeline.EventDownloaderConnect,
			fmt.Sprintf("Connected to downloader: %s", name),
			"",
			"",
			"",
			name,
			map[string]any{
				"type": dl.Type(),
			},
		)
	}

	// Test connections to all apps
	for name, a := range o.apps.All() {
		if err := a.TestConnection(o.ctx); err != nil {
			o.logger.Warn().Err(err).Str("app", name).Msg("failed to connect to app")
		} else {
			o.recordEvent(timeline.EventAppConnected, fmt.Sprintf("Connected to app: %s", name), "", "", name, "", map[string]any{
				"type":     a.Type(),
				"category": a.Category(),
			})
		}
	}

	// Start polling loop
	o.wg.Go(o.pollLoop)

	o.logger.Info().Msg("orchestrator started")
	return nil
}

// Stop stops the orchestrator with a timeout for graceful shutdown.
func (o *Orchestrator) Stop() {
	if o.cancel != nil {
		o.cancel()
	}

	// Wait for goroutines with a timeout to prevent hanging on stuck processes
	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		o.logger.Debug().Msg("all goroutines completed cleanly")
	case <-time.After(defaultShutdownTimeout):
		o.logger.Warn().Msg("timeout waiting for goroutines, some processes may still be running")
	}

	// Close downloaders
	for _, dl := range o.downloaders.All() {
		if err := dl.Close(); err != nil {
			o.logger.Warn().Err(err).Msg("error closing downloader")
		}
	}

	o.logger.Info().Msg("orchestrator stopped")
}

func (o *Orchestrator) pollLoop() {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	// Initial poll
	o.poll()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			o.poll()
		}
	}
}

func (o *Orchestrator) poll() {
	o.logger.Debug().Msg("polling downloaders")

	// Track which downloads we've seen in this poll cycle
	seenKeys := make(map[string]bool)

	for name, dl := range o.downloaders.All() {
		// Get categories this downloader handles
		categories := o.getCategoriesForDownloader(name)

		o.logger.Debug().
			Str("downloader", name).
			Strs("categories", categories).
			Msg("listing downloads")

		// List downloads - if no categories configured, list all
		downloads, err := dl.ListDownloads(o.ctx, categories)
		if err != nil {
			o.logger.Error().Err(err).Str("downloader", name).Msg("failed to list downloads")
			continue
		}

		o.logger.Debug().
			Str("downloader", name).
			Int("count", len(downloads)).
			Msg("found downloads")

		for _, item := range downloads {
			key := fmt.Sprintf("%s:%s", name, item.ID)
			seenKeys[key] = true

			o.logger.Debug().
				Str("name", item.Name).
				Str("category", item.Category).
				Str("state", string(item.State)).
				Float64("progress", item.Progress).
				Msg("processing download")
			o.processDownload(name, dl, &item)
		}
	}

	// Check for category changes and removed downloads
	o.checkForChangesAndRemovals(seenKeys)
}

func (o *Orchestrator) getCategoriesForDownloader(_ string) []string {
	var categories []string
	for _, a := range o.apps.All() {
		// Check if this app is configured to use this downloader
		// For now, we assume all apps can use any downloader
		categories = append(categories, a.Category())
	}
	return categories
}

func (o *Orchestrator) processDownload(downloaderName string, dl download.Downloader, dlInfo *download.Download) {
	key := fmt.Sprintf("%s:%s", downloaderName, dlInfo.ID)

	o.trackedMu.Lock()
	tracked, exists := o.tracked[key]
	if !exists {
		// Find apps for this category
		apps := o.apps.GetByCategory(dlInfo.Category)

		// Only track downloads that have a matching app
		if len(apps) == 0 {
			o.trackedMu.Unlock()
			o.logger.Debug().
				Str("download", dlInfo.Name).
				Str("category", dlInfo.Category).
				Msg("no apps for category, skipping")
			return
		}

		// Fetch file list to get file count for UI display
		files, err := dl.GetFiles(o.ctx, dlInfo.ID)
		if err != nil {
			o.logger.Warn().Err(err).Str("download", dlInfo.Name).Msg("failed to get files on discovery")
		} else {
			dlInfo.Files = files
		}

		// New download
		tracked = &TrackedDownload{
			Download:         dlInfo,
			DownloaderName:   downloaderName,
			OriginalCategory: dlInfo.Category,
			State:            StateDiscovered,
			DiscoveredAt:     time.Now(),
			Apps:             apps,
		}

		o.tracked[key] = tracked
		o.logger.Info().
			Str("download", dlInfo.Name).
			Str("category", dlInfo.Category).
			Str("downloader", downloaderName).
			Int("apps", len(tracked.Apps)).
			Int("files", len(dlInfo.Files)).
			Msg("discovered new download")

		// Record discovery event
		o.recordEvent(
			timeline.EventDiscovered,
			fmt.Sprintf("Discovered: %s", dlInfo.Name),
			dlInfo.ID,
			dlInfo.Name,
			"",
			downloaderName,
			map[string]any{
				"category":   dlInfo.Category,
				"file_count": len(dlInfo.Files),
				"size":       dlInfo.Size,
			},
		)
	}
	o.trackedMu.Unlock()

	// Check for category change before updating
	tracked.mu.RLock()
	originalCategory := tracked.OriginalCategory
	tracked.mu.RUnlock()

	if dlInfo.Category != originalCategory {
		// Category changed - handle it
		o.handleCategoryChanged(tracked, key, dlInfo.Category)
		return
	}

	// Update download info
	tracked.mu.Lock()
	existingFiles := tracked.Download.Files
	tracked.Download = dlInfo

	// Refresh file info if download is still in progress on seedbox
	// This keeps file download progress up to date for the UI
	//nolint:nestif // error recovery logic requires nested conditions
	if dlInfo.State == download.TorrentStateDownloading || dlInfo.State == download.TorrentStatePaused {
		files, err := dl.GetFiles(o.ctx, dlInfo.ID)
		if err != nil {
			o.logger.Debug().Err(err).Str("download", dlInfo.Name).Msg("failed to refresh files")
			// Keep existing files on error
			if len(existingFiles) > 0 {
				tracked.Download.Files = existingFiles
			}
		} else {
			tracked.Download.Files = files
		}
	} else if len(existingFiles) > 0 && len(dlInfo.Files) == 0 {
		// Preserve existing files if not refreshing
		tracked.Download.Files = existingFiles
	}
	tracked.mu.Unlock()

	// Process based on state
	o.advanceState(tracked, dl)
}

func (o *Orchestrator) advanceState(tracked *TrackedDownload, dl download.Downloader) {
	tracked.mu.Lock()
	currentState := tracked.State
	tracked.mu.Unlock()

	switch currentState {
	case StateDiscovered:
		// Check if download has any completed files to sync
		o.startSyncing(tracked, dl)

	case StateSyncing:
		// Continue syncing (check for newly completed files)
		o.continueSyncing(tracked, dl)

	case StateSynced:
		// Move to final location
		o.moveToFinal(tracked)

	case StateMoving:
		// Wait for move to complete (handled synchronously in moveToFinal)

	case StateImporting:
		// Trigger import in media apps
		o.triggerImport(tracked)

	case StateComplete:
		// Check if we should clean up
		o.cleanup(tracked)

	case StateError:
		// Log and potentially retry
		o.handleError(tracked)
	}
}

func (o *Orchestrator) startSyncing(tracked *TrackedDownload, dl download.Downloader) {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()

	// Get file states
	files, err := dl.GetFiles(o.ctx, tracked.Download.ID)
	if err != nil {
		o.logger.Error().Err(err).Str("download", tracked.Download.Name).Msg("failed to get files")
		return
	}

	// Count complete vs incomplete files
	completeCount := 0
	incompleteCount := 0
	for _, f := range files {
		if f.Priority == 0 {
			continue // Skip files not selected for download
		}
		if f.State == download.FileStateComplete {
			completeCount++
		} else {
			incompleteCount++
		}
	}

	if completeCount == 0 {
		o.logger.Debug().
			Str("download", tracked.Download.Name).
			Int("incomplete_files", incompleteCount).
			Msg("no complete files yet, waiting")
		return
	}

	o.logger.Info().
		Str("download", tracked.Download.Name).
		Int("complete_files", completeCount).
		Int("incomplete_files", incompleteCount).
		Msg("starting incremental sync for complete files")

	// Determine final path
	var finalPath string
	if len(tracked.Apps) > 0 {
		a := tracked.Apps[0]
		if a.DownloadsPath() != "" {
			finalPath = a.DownloadsPath()
		} else {
			finalPath = filepath.Join(o.downloadsPath, tracked.DownloaderName, tracked.Download.Category)
		}
	} else {
		finalPath = filepath.Join(o.downloadsPath, tracked.DownloaderName, tracked.Download.Category)
	}

	// Store files in download for API access
	tracked.Download.Files = files

	// Check if files already exist at the final destination
	// This can happen after a restart if files were already synced
	allExist, existingFiles, missingFiles := o.checkFilesAtFinal(tracked.Download, files, finalPath)
	if allExist {
		o.logger.Info().
			Str("download", tracked.Download.Name).
			Int("files", len(files)).
			Str("path", finalPath).
			Msg("all files already exist at final destination, skipping sync")

		// Mark as complete - no need to sync or move
		tracked.State = StateComplete
		tracked.CompletedAt = time.Now()
		return
	}

	if len(existingFiles) > 0 {
		o.logger.Info().
			Str("download", tracked.Download.Name).
			Int("existing", len(existingFiles)).
			Int("missing", len(missingFiles)).
			Msg("some files already exist at final destination")
	}

	// Create sync job
	tracked.SyncDownload = o.syncer.CreateSyncDownload(tracked.Download, tracked.DownloaderName, finalPath)
	tracked.State = StateSyncing

	o.logger.Info().
		Str("download", tracked.Download.Name).
		Int("files", len(files)).
		Msg("starting sync")

	// Record sync started event
	o.recordEvent(
		timeline.EventSyncStarted,
		fmt.Sprintf("Sync started: %s", tracked.Download.Name),
		tracked.Download.ID,
		tracked.Download.Name,
		"",
		tracked.DownloaderName,
		map[string]any{
			"file_count": len(files),
			"final_path": finalPath,
		},
	)

	// Start sync in background - track in WaitGroup so Stop() waits for completion
	o.wg.Go(func() {
		if syncErr := o.syncer.Sync(o.ctx, dl, tracked.SyncDownload); syncErr != nil {
			o.logger.Error().Err(syncErr).Str("download", tracked.Download.Name).Msg("sync error")
		}
	})
}

// checkFilesAtFinal checks if files already exist at the final destination with correct sizes.
// Returns: allExist (bool), existingFiles (paths), missingFiles (paths).
func (o *Orchestrator) checkFilesAtFinal(
	_ *download.Download, files []download.File, finalPath string,
) (bool, []string, []string) {
	var existingFiles []string
	var missingFiles []string

	for _, f := range files {
		// Skip files not selected for download
		if f.Priority == 0 {
			continue
		}

		// Build the final file path (matches MoveToFinal logic)
		// Note: f.Path already includes the torrent name as the first component
		// Use SafeJoin to prevent path traversal attacks
		finalFilePath, err := fileutil.SafeJoin(finalPath, f.Path)
		if err != nil {
			o.logger.Warn().
				Str("file", f.Path).
				Err(err).
				Msg("skipping file with invalid path")
			continue
		}

		info, err := os.Stat(finalFilePath)
		if err != nil {
			// File doesn't exist
			missingFiles = append(missingFiles, f.Path)
			continue
		}

		// Check if size matches
		if info.Size() == f.Size {
			existingFiles = append(existingFiles, f.Path)
		} else {
			// File exists but size doesn't match - needs re-sync
			o.logger.Warn().
				Str("file", f.Path).
				Int64("expected", f.Size).
				Int64("actual", info.Size()).
				Msg("file exists but size mismatch, will re-sync")
			missingFiles = append(missingFiles, f.Path)
		}
	}

	// All files exist if there are no missing files and at least one existing file
	allExist := len(missingFiles) == 0 && len(existingFiles) > 0

	return allExist, existingFiles, missingFiles
}

func (o *Orchestrator) continueSyncing(tracked *TrackedDownload, dl download.Downloader) {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()

	if tracked.SyncDownload == nil {
		tracked.State = StateDiscovered
		return
	}

	// Get current progress
	completedSize, status := tracked.SyncDownload.GetProgress()

	switch status {
	case filesync.FileStatusComplete:
		tracked.State = StateSynced
		o.logger.Info().
			Str("download", tracked.Download.Name).
			Msg("sync complete")

		// Record sync complete event
		o.recordEvent(
			timeline.EventSyncComplete,
			fmt.Sprintf("Sync complete: %s", tracked.Download.Name),
			tracked.Download.ID,
			tracked.Download.Name,
			"",
			tracked.DownloaderName,
			map[string]any{
				"completed_size": completedSize,
			},
		)

	case filesync.FileStatusError:
		tracked.State = StateError
		tracked.Error = tracked.SyncDownload.Error
		o.logger.Error().
			Err(tracked.Error).
			Str("download", tracked.Download.Name).
			Msg("sync error")

		// Record error event
		errMsg := ""
		if tracked.Error != nil {
			errMsg = tracked.Error.Error()
		}
		o.recordEvent(
			timeline.EventError,
			fmt.Sprintf("Sync error: %s", tracked.Download.Name),
			tracked.Download.ID,
			tracked.Download.Name,
			"",
			tracked.DownloaderName,
			map[string]any{
				"error": errMsg,
			},
		)

	case filesync.FileStatusSyncing:
		// Sync is in progress, wait for it to complete
		o.logger.Debug().
			Str("download", tracked.Download.Name).
			Int64("completed_bytes", completedSize).
			Msg("sync in progress")

	case filesync.FileStatusPending:
		// Some files still pending - sync newly completed ones
		o.logger.Debug().
			Str("download", tracked.Download.Name).
			Int64("completed_bytes", completedSize).
			Msg("checking for newly completed files to sync")

		// Track in WaitGroup so Stop() waits for completion
		o.wg.Go(func() {
			if err := o.syncer.Sync(o.ctx, dl, tracked.SyncDownload); err != nil {
				o.logger.Error().Err(err).Str("download", tracked.Download.Name).Msg("sync error")
			}
		})

	case filesync.FileStatusSkipped:
		// All files were skipped (already exist at destination)
		tracked.State = StateSynced
	}
}

func (o *Orchestrator) moveToFinal(tracked *TrackedDownload) {
	tracked.mu.Lock()
	tracked.State = StateMoving
	job := tracked.SyncDownload
	tracked.mu.Unlock()

	if job == nil {
		tracked.mu.Lock()
		tracked.State = StateError
		tracked.Error = errors.New("no sync job")
		tracked.mu.Unlock()
		return
	}

	if err := o.syncer.MoveToFinal(job); err != nil {
		tracked.mu.Lock()
		tracked.State = StateError
		tracked.Error = err
		tracked.mu.Unlock()
		o.logger.Error().Err(err).Str("download", tracked.Download.Name).Msg("move error")
		return
	}

	tracked.mu.Lock()
	tracked.State = StateImporting
	tracked.mu.Unlock()

	o.logger.Info().
		Str("download", tracked.Download.Name).
		Str("path", job.FinalPath).
		Msg("moved to final location")

	// Record move complete event
	o.recordEvent(
		timeline.EventMoveComplete,
		fmt.Sprintf("Moved to final location: %s", tracked.Download.Name),
		tracked.Download.ID,
		tracked.Download.Name,
		"",
		tracked.DownloaderName,
		map[string]any{
			"path": job.FinalPath,
		},
	)
}

func (o *Orchestrator) triggerImport(tracked *TrackedDownload) {
	tracked.mu.Lock()
	apps := tracked.Apps
	job := tracked.SyncDownload
	downloadID := tracked.Download.ID
	downloadName := tracked.Download.Name
	downloaderName := tracked.DownloaderName
	tracked.mu.Unlock()

	if job == nil {
		return
	}

	if len(apps) == 0 {
		o.logger.Info().
			Str("download", downloadName).
			Str("path", job.FinalPath).
			Msg("sync complete, no apps to trigger import")
	} else {
		importPath := filepath.Join(job.FinalPath, filepath.Base(downloadName))

		for _, a := range apps {
			// Record import started event
			o.recordEvent(timeline.EventImportStarted, fmt.Sprintf("Import started: %s -> %s", downloadName, a.Name()), downloadID, downloadName, a.Name(), downloaderName, map[string]any{
				"path": importPath,
			})

			if err := a.TriggerImport(o.ctx, importPath); err != nil {
				o.logger.Error().
					Err(err).
					Str("download", downloadName).
					Str("app", a.Name()).
					Msg("import trigger error")

				// Record import failed event
				o.recordEvent(timeline.EventImportFailed, fmt.Sprintf("Import failed: %s -> %s", downloadName, a.Name()), downloadID, downloadName, a.Name(), downloaderName, map[string]any{
					"path":  importPath,
					"error": err.Error(),
				})
			} else {
				o.logger.Info().
					Str("download", downloadName).
					Str("app", a.Name()).
					Msg("triggered import")

				// Record import complete event
				o.recordEvent(timeline.EventImportComplete, fmt.Sprintf("Import complete: %s -> %s", downloadName, a.Name()), downloadID, downloadName, a.Name(), downloaderName, map[string]any{
					"path": importPath,
				})
			}
		}
	}

	tracked.mu.Lock()
	tracked.State = StateComplete
	tracked.CompletedAt = time.Now()
	tracked.mu.Unlock()

	// Record complete event
	o.recordEvent(
		timeline.EventComplete,
		fmt.Sprintf("Complete: %s", downloadName),
		downloadID,
		downloadName,
		"",
		downloaderName,
		nil,
	)
}

func (o *Orchestrator) cleanup(tracked *TrackedDownload) {
	// For now, just remove from tracking after some time
	tracked.mu.RLock()
	completedAt := tracked.CompletedAt
	tracked.mu.RUnlock()

	if time.Since(completedAt) > 24*time.Hour {
		key := fmt.Sprintf("%s:%s", tracked.DownloaderName, tracked.Download.ID)
		o.trackedMu.Lock()
		delete(o.tracked, key)
		o.trackedMu.Unlock()

		if tracked.SyncDownload != nil {
			o.syncer.RemoveByKey(tracked.DownloaderName, tracked.Download.ID)
		}
	}
}

func (o *Orchestrator) handleError(tracked *TrackedDownload) {
	// Log the error, could implement retry logic here
	tracked.mu.RLock()
	err := tracked.Error
	tracked.mu.RUnlock()

	o.logger.Error().
		Err(err).
		Str("download", tracked.Download.Name).
		Msg("download in error state")
}

// GetTrackedDownloads returns all tracked downloads.
func (o *Orchestrator) GetTrackedDownloads() []*TrackedDownload {
	o.trackedMu.RLock()
	defer o.trackedMu.RUnlock()

	downloads := make([]*TrackedDownload, 0, len(o.tracked))
	for _, td := range o.tracked {
		downloads = append(downloads, td)
	}
	return downloads
}

// checkForChangesAndRemovals checks for downloads that have changed category or been removed.
func (o *Orchestrator) checkForChangesAndRemovals(seenKeys map[string]bool) {
	o.trackedMu.RLock()
	// Collect keys we need to check
	var keysToCheck []string
	trackedCopy := make(map[string]*TrackedDownload)
	for key, tracked := range o.tracked {
		if !seenKeys[key] {
			keysToCheck = append(keysToCheck, key)
			trackedCopy[key] = tracked
		}
	}
	o.trackedMu.RUnlock()

	if len(keysToCheck) == 0 {
		return
	}

	for _, key := range keysToCheck {
		tracked := trackedCopy[key]
		tracked.mu.RLock()
		downloaderName := tracked.DownloaderName
		downloadID := tracked.Download.ID
		downloadName := tracked.Download.Name
		originalCategory := tracked.OriginalCategory
		tracked.mu.RUnlock()

		// Get the downloader
		dl, ok := o.downloaders.Get(downloaderName)
		if !ok {
			continue
		}

		// Query the downloader to see if the download still exists
		download, err := dl.GetDownload(o.ctx, downloadID)
		if err != nil {
			// Download was removed from downloader
			o.handleDownloadRemoved(tracked, key)
			continue
		}

		// Download still exists - check if category changed
		if download.Category != originalCategory {
			o.handleCategoryChanged(tracked, key, download.Category)
		} else {
			// Category didn't change, download just not in our filtered list
			// This could happen if we filter by category in ListDownloads
			o.logger.Debug().
				Str("download", downloadName).
				Str("category", download.Category).
				Msg("download exists but not in tracked categories")
		}
	}
}

// handleDownloadRemoved handles when a download is removed from the download.
func (o *Orchestrator) handleDownloadRemoved(tracked *TrackedDownload, key string) {
	tracked.mu.RLock()
	downloadName := tracked.Download.Name
	downloadID := tracked.Download.ID
	apps := tracked.Apps
	job := tracked.SyncDownload
	state := tracked.State
	downloaderName := tracked.DownloaderName
	tracked.mu.RUnlock()

	o.logger.Info().
		Str("download", downloadName).
		Str("state", string(state)).
		Msg("download removed from downloader")

	// Record removed event
	o.recordEvent(
		timeline.EventRemoved,
		fmt.Sprintf("Removed: %s", downloadName),
		downloadID,
		downloadName,
		"",
		downloaderName,
		map[string]any{
			"state": string(state),
		},
	)

	// If still syncing, cancel the sync job first
	if state != StateComplete && job != nil {
		o.logger.Info().
			Str("download", downloadName).
			Msg("cancelling incomplete sync")
		if err := o.syncer.CancelByKey(downloaderName, downloadID); err != nil {
			o.logger.Warn().Err(err).Str("download", downloadName).Msg("error cancelling sync job")
		}
		// CancelJob cleans up staging files, now cleanup any final files if they exist
		o.cleanupSyncedFiles(tracked, "removed while syncing")
	} else if state == StateComplete {
		// Check if any app wants cleanup on remove
		shouldCleanup := false
		for _, a := range apps {
			if a.CleanupOnRemove() {
				shouldCleanup = true
				break
			}
		}

		if shouldCleanup && job != nil {
			o.cleanupSyncedFiles(tracked, "removed from downloader")
		}
	}

	// Remove from tracking
	o.removeFromTracking(tracked, key)
}

// handleCategoryChanged handles when a download's category changes.
func (o *Orchestrator) handleCategoryChanged(tracked *TrackedDownload, key string, newCategory string) {
	tracked.mu.RLock()
	downloadName := tracked.Download.Name
	downloadID := tracked.Download.ID
	originalCategory := tracked.OriginalCategory
	apps := tracked.Apps
	job := tracked.SyncDownload
	state := tracked.State
	downloaderName := tracked.DownloaderName
	tracked.mu.RUnlock()

	o.logger.Info().
		Str("download", downloadName).
		Str("old_category", originalCategory).
		Str("new_category", newCategory).
		Str("state", string(state)).
		Msg("download category changed")

	// Record category changed event
	o.recordEvent(
		timeline.EventCategoryChanged,
		fmt.Sprintf("Category changed: %s (%s -> %s)", downloadName, originalCategory, newCategory),
		downloadID,
		downloadName,
		"",
		downloaderName,
		map[string]any{
			"old_category": originalCategory,
			"new_category": newCategory,
			"state":        string(state),
		},
	)

	// Check if new category belongs to another app
	newApps := o.apps.GetByCategory(newCategory)

	if len(newApps) > 0 {
		if state == StateComplete {
			// Download is complete - move files and trigger import
			// This works even if job is nil (e.g., after restart when files already existed)
			o.handleCategoryMigration(tracked, key, newCategory, newApps)
			return
		} else if job != nil {
			// Still syncing - update the job's final path and apps, continue syncing
			o.handleCategoryMigrationWhileSyncing(tracked, newCategory, newApps)
			return
		}
	}

	// New category doesn't match any app
	if state != StateComplete && job != nil {
		// Still syncing to untracked category - cancel and cleanup
		o.logger.Info().
			Str("download", downloadName).
			Msg("cancelling incomplete sync due to category change to untracked category")
		if err := o.syncer.CancelByKey(downloaderName, downloadID); err != nil {
			o.logger.Warn().Err(err).Str("download", downloadName).Msg("error cancelling sync job")
		}
		o.cleanupSyncedFiles(tracked, "category changed while syncing")
		o.removeFromTracking(tracked, key)
		return
	}

	// Complete but new category doesn't match any app - cleanup if configured
	shouldCleanup := false
	for _, a := range apps {
		if a.CleanupOnCategoryChange() {
			shouldCleanup = true
			break
		}
	}

	if shouldCleanup && job != nil {
		o.cleanupSyncedFiles(tracked, "category changed")
	}

	// Remove from tracking
	o.removeFromTracking(tracked, key)
}

// handleCategoryMigrationWhileSyncing updates a syncing job to use a new app's path.
func (o *Orchestrator) handleCategoryMigrationWhileSyncing(
	tracked *TrackedDownload, newCategory string, newApps []app.App,
) {
	tracked.mu.Lock()
	downloadName := tracked.Download.Name
	job := tracked.SyncDownload
	oldApps := tracked.Apps

	// Update the tracked download to use the new apps
	tracked.Apps = newApps
	tracked.OriginalCategory = newCategory

	// Update the job's final path to the new app's location
	if job != nil && len(newApps) > 0 {
		newApp := newApps[0]
		newFinalPath := filepath.Join(newApp.DownloadsPath(), tracked.DownloaderName)
		oldFinalPath := job.GetFinalPath()
		job.UpdateDestination(newFinalPath, newCategory)

		o.logger.Info().
			Str("download", downloadName).
			Str("old_path", oldFinalPath).
			Str("new_path", newFinalPath).
			Str("new_app", newApp.Name()).
			Strs("old_apps", appNames(oldApps)).
			Msg("updated syncing job to new app")
	}

	tracked.mu.Unlock()
}

// appNames returns the names of apps as a string slice.
func appNames(apps []app.App) []string {
	names := make([]string, len(apps))
	for i, a := range apps {
		names[i] = a.Name()
	}
	return names
}

// handleCategoryMigration moves synced files to a new app and triggers import.
func (o *Orchestrator) handleCategoryMigration(tracked *TrackedDownload, key string, _ string, newApps []app.App) {
	tracked.mu.RLock()
	downloadName := tracked.Download.Name
	job := tracked.SyncDownload
	apps := tracked.Apps
	tracked.mu.RUnlock()

	if len(newApps) == 0 {
		o.removeFromTracking(tracked, key)
		return
	}

	// Calculate source path where files currently are
	// If we have a sync job, use its FinalPath; otherwise calculate from apps or default path
	var oldBasePath string
	switch {
	case job != nil:
		oldBasePath = job.FinalPath
	case len(apps) > 0 && apps[0].DownloadsPath() != "":
		oldBasePath = apps[0].DownloadsPath()
	default:
		oldBasePath = filepath.Join(o.downloadsPath, tracked.DownloaderName, tracked.OriginalCategory)
	}

	// Use the first new app's downloads path as the destination
	newApp := newApps[0]
	newFinalPath := filepath.Join(newApp.DownloadsPath(), tracked.DownloaderName)

	// Source path where files currently are
	oldPath := filepath.Join(oldBasePath, filepath.Base(downloadName))
	newPath := filepath.Join(newFinalPath, filepath.Base(downloadName))

	o.logger.Info().
		Str("download", downloadName).
		Str("from", oldPath).
		Str("to", newPath).
		Str("new_app", newApp.Name()).
		Msg("migrating synced files to new app")

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(newPath), 0750); err != nil {
		o.logger.Error().
			Err(err).
			Str("path", newPath).
			Msg("failed to create destination directory for migration")
		o.removeFromTracking(tracked, key)
		return
	}

	// Move files to new location
	if renameErr := os.Rename(oldPath, newPath); renameErr != nil {
		// If rename fails (cross-device), try to recursively copy and delete
		o.logger.Warn().
			Err(renameErr).
			Msg("rename failed, attempting copy")
		if copyErr := copyDir(oldPath, newPath); copyErr != nil {
			o.logger.Error().
				Err(copyErr).
				Str("from", oldPath).
				Str("to", newPath).
				Msg("failed to migrate files")
			o.removeFromTracking(tracked, key)
			return
		}
		if removeErr := os.RemoveAll(oldPath); removeErr != nil {
			o.logger.Warn().
				Err(removeErr).
				Str("path", oldPath).
				Msg("failed to remove old path after migration")
		}
	}

	o.logger.Info().
		Str("download", downloadName).
		Str("path", newPath).
		Msg("files migrated successfully")

	// Trigger import on all new apps
	for _, a := range newApps {
		if err := a.TriggerImport(o.ctx, newPath); err != nil {
			o.logger.Error().
				Err(err).
				Str("download", downloadName).
				Str("app", a.Name()).
				Msg("failed to trigger import on new app")
		} else {
			o.logger.Info().
				Str("download", downloadName).
				Str("app", a.Name()).
				Msg("triggered import on new app")
		}
	}

	// Remove from tracking
	o.removeFromTracking(tracked, key)
}

// cleanupSyncedFiles removes the synced files for a tracked download.
func (o *Orchestrator) cleanupSyncedFiles(tracked *TrackedDownload, reason string) {
	tracked.mu.RLock()
	job := tracked.SyncDownload
	downloadName := tracked.Download.Name
	downloadID := tracked.Download.ID
	downloaderName := tracked.DownloaderName
	tracked.mu.RUnlock()

	if job == nil {
		return
	}

	// The final path is where files were moved to
	// Files are at: FinalPath/torrentName/...
	cleanupPath := filepath.Join(job.FinalPath, filepath.Base(downloadName))

	o.logger.Info().
		Str("download", downloadName).
		Str("path", cleanupPath).
		Str("reason", reason).
		Msg("cleaning up synced files")

	if err := os.RemoveAll(cleanupPath); err != nil {
		o.logger.Error().
			Err(err).
			Str("download", downloadName).
			Str("path", cleanupPath).
			Msg("failed to cleanup synced files")
	} else {
		o.logger.Info().
			Str("download", downloadName).
			Str("path", cleanupPath).
			Msg("cleaned up synced files")

		// Record cleanup event
		o.recordEvent(timeline.EventCleanup, fmt.Sprintf("Cleaned up: %s", downloadName), downloadID, downloadName, "", downloaderName, map[string]any{
			"path":   cleanupPath,
			"reason": reason,
		})
	}
}

// removeFromTracking removes a download from the tracked map and filesync.
func (o *Orchestrator) removeFromTracking(tracked *TrackedDownload, key string) {
	tracked.mu.RLock()
	downloadID := tracked.Download.ID
	downloaderName := tracked.DownloaderName
	syncJob := tracked.SyncDownload
	tracked.mu.RUnlock()

	o.trackedMu.Lock()
	delete(o.tracked, key)
	o.trackedMu.Unlock()

	if syncJob != nil {
		o.syncer.RemoveByKey(downloaderName, downloadID)
	}
}

// recordEvent records an event to the timeline if configured.
func (o *Orchestrator) recordEvent(
	eventType timeline.EventType,
	message string,
	downloadID, downloadName, appName, downloaderName string,
	details map[string]any,
) {
	if o.timeline == nil {
		return
	}

	o.timeline.Record(timeline.Event{
		Type:         eventType,
		Message:      message,
		DownloadID:   downloadID,
		DownloadName: downloadName,
		AppName:      appName,
		Downloader:   downloaderName,
		Details:      details,
	})
}

// GetTimeline returns the timeline recorder.
func (o *Orchestrator) GetTimeline() timeline.Recorder {
	return o.timeline
}

// GetStats returns orchestrator statistics.
func (o *Orchestrator) GetStats() map[string]any {
	o.trackedMu.RLock()
	defer o.trackedMu.RUnlock()

	stats := map[string]any{
		"total_tracked": len(o.tracked),
		"by_state":      make(map[string]int),
	}

	byState, _ := stats["by_state"].(map[string]int)
	downloadingOnSeedbox := 0
	pausedOnSeedbox := 0

	for _, td := range o.tracked {
		td.mu.RLock()
		state := td.State
		dlState := td.Download.State
		syncDownload := td.SyncDownload
		td.mu.RUnlock()

		// For consistency with the downloads API, use sync download status when available
		// This ensures stats reflect the actual current state shown in the UI
		effectiveState := string(state)
		if syncDownload != nil && state == StateSyncing {
			// Check the sync download's actual status
			_, syncStatus := syncDownload.GetProgress()
			switch syncStatus {
			case filesync.FileStatusError:
				effectiveState = string(StateError)
			case filesync.FileStatusComplete:
				effectiveState = string(StateSynced)
			case filesync.FileStatusPending, filesync.FileStatusSyncing, filesync.FileStatusSkipped:
				// Keep the current state (syncing)
			}
		}
		byState[effectiveState]++

		// Track seedbox-specific states
		switch dlState {
		case download.TorrentStateDownloading:
			downloadingOnSeedbox++
		case download.TorrentStatePaused:
			pausedOnSeedbox++
		case download.TorrentStateComplete, download.TorrentStateError:
			// No action needed for these states
		}
	}

	stats["downloading_on_seedbox"] = downloadingOnSeedbox
	stats["paused_on_seedbox"] = pausedOnSeedbox

	return stats
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate the destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return fileutil.CopyFile(path, dstPath)
	})
}
