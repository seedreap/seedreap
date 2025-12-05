// Package filesync handles file synchronization from remote to local storage.
package filesync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/fileutil"
	"github.com/seedreap/seedreap/internal/transfer"
)

// FileStatus represents the sync status of a file.
type FileStatus string

const (
	// FileStatusPending indicates the file is waiting to be synced.
	FileStatusPending FileStatus = "pending"
	// FileStatusSyncing indicates the file is currently being synced.
	FileStatusSyncing FileStatus = "syncing"
	// FileStatusComplete indicates the file has been synced.
	FileStatusComplete FileStatus = "complete"
	// FileStatusError indicates an error occurred during sync.
	FileStatusError FileStatus = "error"
	// FileStatusSkipped indicates the file was skipped (already exists, priority 0, etc).
	FileStatusSkipped FileStatus = "skipped"
)

// Default configuration values.
const (
	defaultMaxConcurrent = 2
)

// syncKey creates a composite key for the downloads map from downloader name and download ID.
// This ensures downloads are unique per-downloader, allowing the same torrent to be
// synced from different downloaders independently.
func syncKey(downloaderName, downloadID string) string {
	return downloaderName + ":" + downloadID
}

// FileProgress tracks the progress of syncing a single file.
type FileProgress struct {
	Path        string
	RemotePath  string
	LocalPath   string
	Size        int64
	Transferred int64
	Status      FileStatus
	Error       error
	StartedAt   time.Time
	CompletedAt time.Time
	BytesPerSec int64
	mu          sync.RWMutex
}

// Progress returns a snapshot of the file progress.
//
//nolint:nonamedreturns // named returns document multiple int64 values
func (fp *FileProgress) Progress() (transferred int64, bytesPerSec int64) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	return fp.Transferred, fp.BytesPerSec
}

// SetProgress updates the progress atomically.
func (fp *FileProgress) SetProgress(transferred, bytesPerSec int64) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.Transferred = transferred
	fp.BytesPerSec = bytesPerSec
}

// GetStatus returns the current status.
func (fp *FileProgress) GetStatus() FileStatus {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	return fp.Status
}

// FileProgressSnapshot is a point-in-time snapshot of file progress.
type FileProgressSnapshot struct {
	Path        string
	Size        int64
	Transferred int64
	Status      FileStatus
	BytesPerSec int64
}

// Snapshot returns a point-in-time snapshot of the file progress.
func (fp *FileProgress) Snapshot() FileProgressSnapshot {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	return FileProgressSnapshot{
		Path:        fp.Path,
		Size:        fp.Size,
		Transferred: fp.Transferred,
		Status:      fp.Status,
		BytesPerSec: fp.BytesPerSec,
	}
}

// SyncDownload represents a download being synced.
type SyncDownload struct {
	ID            string // Download ID/hash
	Name          string
	Downloader    string
	Category      string
	RemoteBase    string // Base path on remote
	LocalBase     string // Local syncing path
	FinalPath     string // Where to move after complete
	Files         []*FileProgress
	TotalSize     int64
	TotalFiles    int
	CompletedSize int64
	Status        FileStatus
	Error         error
	StartedAt     time.Time
	CompletedAt   time.Time
	CancelledAt   time.Time
	mu            sync.RWMutex

	// Per-job context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// GetProgress returns the current progress of the download sync.
//
//nolint:nonamedreturns // named returns document return values
func (sd *SyncDownload) GetProgress() (completedSize int64, status FileStatus) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	var completed int64
	for _, f := range sd.Files {
		transferred, _ := f.Progress()
		completed += transferred
	}

	return completed, sd.Status
}

// Cancel cancels the download sync.
func (sd *SyncDownload) Cancel() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.cancel != nil {
		sd.cancel()
		sd.CancelledAt = time.Now()
		sd.Status = FileStatusError
		sd.Error = context.Canceled
	}
}

// IsCancelled returns true if the download sync has been cancelled.
func (sd *SyncDownload) IsCancelled() bool {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	return !sd.CancelledAt.IsZero()
}

// Context returns the download sync's context.
func (sd *SyncDownload) Context() context.Context {
	return sd.ctx
}

// UpdateDestination updates the download's final path and category.
// This is used when a download's category changes to another tracked app while syncing.
func (sd *SyncDownload) UpdateDestination(finalPath, category string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.FinalPath = finalPath
	sd.Category = category
}

// GetFinalPath returns the download's final path.
func (sd *SyncDownload) GetFinalPath() string {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	return sd.FinalPath
}

// SyncDownloadSnapshot is a point-in-time snapshot of a download sync.
type SyncDownloadSnapshot struct {
	ID            string
	Name          string
	Downloader    string
	Category      string
	RemoteBase    string
	LocalBase     string
	FinalPath     string
	TotalSize     int64
	TotalFiles    int
	CompletedSize int64
	Status        FileStatus
	Files         []FileProgressSnapshot
	BytesPerSec   int64 // Transfer speed
}

// Snapshot returns a point-in-time snapshot of the download sync.
func (sd *SyncDownload) Snapshot() SyncDownloadSnapshot {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	files := make([]FileProgressSnapshot, len(sd.Files))
	var completedSize int64
	var bytesPerSec int64
	for i, f := range sd.Files {
		files[i] = f.Snapshot()
		completedSize += files[i].Transferred
		// Sum speed from actively syncing files (reported by transfer backend)
		if files[i].Status == FileStatusSyncing {
			bytesPerSec += files[i].BytesPerSec
		}
	}

	return SyncDownloadSnapshot{
		ID:            sd.ID,
		Name:          sd.Name,
		Downloader:    sd.Downloader,
		Category:      sd.Category,
		RemoteBase:    sd.RemoteBase,
		LocalBase:     sd.LocalBase,
		FinalPath:     sd.FinalPath,
		TotalSize:     sd.TotalSize,
		TotalFiles:    sd.TotalFiles,
		CompletedSize: completedSize,
		Status:        sd.Status,
		Files:         files,
		BytesPerSec:   bytesPerSec,
	}
}

// SpeedSample represents a speed measurement at a point in time.
type SpeedSample struct {
	Speed     int64 `json:"speed"`
	Timestamp int64 `json:"timestamp"`
}

// Syncer handles file synchronization from downloaders to local storage.
type Syncer struct {
	syncingPath   string
	maxConcurrent int
	logger        zerolog.Logger
	transferer    transfer.Transferer

	downloads   map[string]*SyncDownload
	downloadsMu sync.RWMutex
	semaphore   chan struct{}

	// Speed history for UI sparkline (last 5 minutes)
	speedHistory   []SpeedSample
	speedHistoryMu sync.RWMutex

	// Callbacks
	onSyncComplete func(sd *SyncDownload)
	onFileComplete func(sd *SyncDownload, file *FileProgress)
}

// Option is a functional option for configuring the syncer.
type Option func(*Syncer)

// WithLogger sets the logger.
func WithLogger(logger zerolog.Logger) Option {
	return func(s *Syncer) {
		s.logger = logger
	}
}

// WithMaxConcurrent sets the maximum concurrent file transfers.
func WithMaxConcurrent(n int) Option {
	return func(s *Syncer) {
		s.maxConcurrent = n
		s.semaphore = make(chan struct{}, n)
	}
}

// WithTransferer sets the transfer backend to use for file transfers.
func WithTransferer(t transfer.Transferer) Option {
	return func(s *Syncer) {
		s.transferer = t
	}
}

// WithOnSyncComplete sets a callback for when a download sync completes.
func WithOnSyncComplete(fn func(sd *SyncDownload)) Option {
	return func(s *Syncer) {
		s.onSyncComplete = fn
	}
}

// WithOnFileComplete sets a callback for when a file completes.
func WithOnFileComplete(fn func(sd *SyncDownload, file *FileProgress)) Option {
	return func(s *Syncer) {
		s.onFileComplete = fn
	}
}

// New creates a new Syncer.
func New(syncingPath string, opts ...Option) *Syncer {
	s := &Syncer{
		syncingPath:   syncingPath,
		maxConcurrent: defaultMaxConcurrent,
		logger:        zerolog.Nop(),
		downloads:     make(map[string]*SyncDownload),
		semaphore:     make(chan struct{}, defaultMaxConcurrent),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// PrepareShutdown prepares for graceful shutdown by suppressing expected errors.
// Call this before cancelling contexts.
func (s *Syncer) PrepareShutdown() {
	if s.transferer != nil {
		s.transferer.PrepareShutdown()
	}
}

// Close releases resources held by the syncer.
func (s *Syncer) Close() error {
	if s.transferer != nil {
		return s.transferer.Close()
	}
	return nil
}

// CreateSyncDownload creates a new sync download for tracking.
func (s *Syncer) CreateSyncDownload(dl *download.Download, downloaderName, finalPath string) *SyncDownload {
	s.downloadsMu.Lock()
	defer s.downloadsMu.Unlock()

	// Check if download already exists (keyed by downloader:id for uniqueness)
	key := syncKey(downloaderName, dl.ID)
	if sd, ok := s.downloads[key]; ok {
		return sd
	}

	// Create per-download context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

	sd := &SyncDownload{
		ID:         dl.ID,
		Name:       dl.Name,
		Downloader: downloaderName,
		Category:   dl.Category,
		RemoteBase: dl.SavePath,
		LocalBase:  filepath.Join(s.syncingPath, downloaderName, dl.ID),
		FinalPath:  finalPath,
		Status:     FileStatusPending,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Create file progress entries
	// Note: f.Path from qBittorrent includes the torrent folder name for multi-file torrents
	// e.g., "TorrentName/file.mkv" - so we join with SavePath, not ContentPath
	for _, f := range dl.Files {
		if f.Priority == 0 {
			continue // Skip files not selected for download
		}

		// Validate file paths to prevent path traversal attacks
		remotePath, err := fileutil.SafeJoin(dl.SavePath, f.Path)
		if err != nil {
			s.logger.Warn().
				Str("file", f.Path).
				Err(err).
				Msg("skipping file with invalid path")
			continue
		}
		localPath, err := fileutil.SafeJoin(sd.LocalBase, f.Path)
		if err != nil {
			s.logger.Warn().
				Str("file", f.Path).
				Err(err).
				Msg("skipping file with invalid path")
			continue
		}
		finalFilePath, err := fileutil.SafeJoin(finalPath, f.Path)
		if err != nil {
			s.logger.Warn().
				Str("file", f.Path).
				Err(err).
				Msg("skipping file with invalid path")
			continue
		}

		fp := &FileProgress{
			Path:       f.Path,
			RemotePath: remotePath,
			LocalPath:  localPath,
			Size:       f.Size,
			Status:     FileStatusPending,
		}

		// Check if file already exists at final destination with correct size
		// This handles the case where a restart happens after sync but before cleanup
		// Note: f.Path already includes the torrent name as the first component
		if info, statErr := os.Stat(finalFilePath); statErr == nil && info.Size() == f.Size {
			fp.Status = FileStatusSkipped
			fp.Transferred = f.Size
			s.logger.Debug().
				Str("file", f.Path).
				Str("final_path", finalFilePath).
				Msg("file already exists at final destination, marking as skipped")
		} else if f.State != download.FileStateComplete {
			// If file is not yet complete in download, mark as pending
			fp.Status = FileStatusPending
		}

		sd.Files = append(sd.Files, fp)
		sd.TotalSize += f.Size
		sd.TotalFiles++
	}

	s.downloads[key] = sd
	return sd
}

// GetByKey returns a sync download by downloader name and download ID.
func (s *Syncer) GetByKey(downloaderName, downloadID string) (*SyncDownload, bool) {
	s.downloadsMu.RLock()
	defer s.downloadsMu.RUnlock()

	sd, ok := s.downloads[syncKey(downloaderName, downloadID)]
	return sd, ok
}

// GetAll returns all sync downloads.
func (s *Syncer) GetAll() []*SyncDownload {
	s.downloadsMu.RLock()
	defer s.downloadsMu.RUnlock()

	downloads := make([]*SyncDownload, 0, len(s.downloads))
	for _, sd := range s.downloads {
		downloads = append(downloads, sd)
	}
	return downloads
}

// RecordSpeed adds a speed sample to the history (called by API on each poll).
func (s *Syncer) RecordSpeed(speed int64) {
	s.speedHistoryMu.Lock()
	defer s.speedHistoryMu.Unlock()

	// Max 100 samples (5 minutes at 3 second intervals)
	const maxSamples = 100

	s.speedHistory = append(s.speedHistory, SpeedSample{
		Speed:     speed,
		Timestamp: time.Now().Unix(),
	})

	// Trim to max size
	if len(s.speedHistory) > maxSamples {
		s.speedHistory = s.speedHistory[len(s.speedHistory)-maxSamples:]
	}
}

// GetSpeedHistory returns the speed history for the sparkline.
func (s *Syncer) GetSpeedHistory() []SpeedSample {
	s.speedHistoryMu.RLock()
	defer s.speedHistoryMu.RUnlock()

	// Return a copy
	result := make([]SpeedSample, len(s.speedHistory))
	copy(result, s.speedHistory)
	return result
}

// GetAggregateSpeed returns the current aggregate transfer speed from the transferer.
func (s *Syncer) GetAggregateSpeed() int64 {
	if s.transferer == nil {
		return 0
	}
	return s.transferer.GetSpeed()
}

// SyncFile syncs a single file from remote to local.
//
//nolint:funlen // file sync requires multiple phases
func (s *Syncer) SyncFile(ctx context.Context, _ download.Downloader, sd *SyncDownload, file *FileProgress) error {
	// Acquire semaphore
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}

	file.mu.Lock()
	file.Status = FileStatusSyncing
	file.StartedAt = time.Now()
	file.mu.Unlock()

	backendName := "unknown"
	if s.transferer != nil {
		backendName = s.transferer.Name()
	}

	s.logger.Debug().
		Str("download", sd.ID).
		Str("file", file.Path).
		Str("remote", file.RemotePath).
		Int64("size", file.Size).
		Str("backend", backendName).
		Msg("starting file sync")

	// Create local directory
	if err := os.MkdirAll(filepath.Dir(file.LocalPath), 0750); err != nil {
		file.mu.Lock()
		file.Status = FileStatusError
		file.Error = err
		file.mu.Unlock()
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Check if file already exists and is complete
	if info, err := os.Stat(file.LocalPath); err == nil {
		if info.Size() == file.Size {
			file.mu.Lock()
			file.Status = FileStatusSkipped
			file.Transferred = file.Size
			file.CompletedAt = time.Now()
			file.mu.Unlock()
			s.logger.Debug().Str("file", file.Path).Msg("file already exists, skipping")
			return nil
		}
	}

	// Transfer the file using the configured backend
	if s.transferer == nil {
		return errors.New("no transfer backend configured")
	}

	req := transfer.Request{
		RemotePath: file.RemotePath,
		LocalPath:  file.LocalPath,
		Size:       file.Size,
	}

	err := s.transferer.Transfer(ctx, req, func(p transfer.Progress) {
		file.SetProgress(p.Transferred, p.BytesPerSec)
	})
	if err != nil {
		file.mu.Lock()
		file.Status = FileStatusError
		file.Error = err
		file.mu.Unlock()
		return fmt.Errorf("transfer failed: %w", err)
	}

	// Verify file was transferred completely
	info, err := os.Stat(file.LocalPath)
	if err != nil {
		file.mu.Lock()
		file.Status = FileStatusError
		file.Error = fmt.Errorf("file not found after transfer: %w", err)
		file.mu.Unlock()
		return file.Error
	}

	if info.Size() != file.Size {
		sizeErr := fmt.Errorf("size mismatch: expected %d, got %d", file.Size, info.Size())
		file.mu.Lock()
		file.Status = FileStatusError
		file.Error = sizeErr
		file.mu.Unlock()
		return sizeErr
	}

	// Update final status
	file.mu.Lock()
	file.Status = FileStatusComplete
	file.Transferred = file.Size
	file.CompletedAt = time.Now()
	elapsed := file.CompletedAt.Sub(file.StartedAt).Seconds()
	if elapsed > 0 {
		file.BytesPerSec = int64(float64(file.Transferred) / elapsed)
	}
	file.mu.Unlock()

	s.logger.Info().
		Str("download", sd.ID).
		Str("file", file.Path).
		Int64("size", file.Size).
		Int64("bps", file.BytesPerSec).
		Msg("file sync complete")

	// Trigger callback
	if s.onFileComplete != nil {
		s.onFileComplete(sd, file)
	}

	return nil
}

// Sync syncs all ready files for a download.
//
//nolint:gocognit,funlen // sync logic requires multiple phases and error handling paths
func (s *Syncer) Sync(ctx context.Context, dl download.Downloader, sd *SyncDownload) error {
	// Check if download sync is already cancelled
	if sd.IsCancelled() {
		return context.Canceled
	}

	// Use the download's context for cancellation, but also respect the parent context
	// Create a merged context that cancels if either is cancelled
	sdCtx := sd.Context()
	mergedCtx, mergedCancel := context.WithCancel(ctx)
	defer mergedCancel()

	// Watch for download sync cancellation
	go func() {
		select {
		case <-sdCtx.Done():
			mergedCancel()
		case <-mergedCtx.Done():
		}
	}()

	sd.mu.Lock()
	sd.Status = FileStatusSyncing
	sd.StartedAt = time.Now()
	sd.mu.Unlock()

	backendName := "unknown"
	if s.transferer != nil {
		backendName = s.transferer.Name()
	}

	s.logger.Info().
		Str("id", sd.ID).
		Str("name", sd.Name).
		Int("files", sd.TotalFiles).
		Str("backend", backendName).
		Msg("starting download sync")

	// Get current file states from downloader
	files, err := dl.GetFiles(mergedCtx, sd.ID)
	if err != nil {
		return fmt.Errorf("failed to get file states: %w", err)
	}

	// Build a map for quick lookup
	fileStates := make(map[string]download.FileState)
	for _, f := range files {
		fileStates[f.Path] = f.State
	}

	// Sync files that are complete in the download
	var wg sync.WaitGroup
	errChan := make(chan error, len(sd.Files))

	for _, file := range sd.Files {
		// Check if file is complete in downloader
		if state, ok := fileStates[file.Path]; ok && state != download.FileStateComplete {
			s.logger.Debug().
				Str("file", file.Path).
				Str("state", string(state)).
				Msg("file not yet complete in downloader, skipping")
			continue
		}

		// Skip already synced files
		file.mu.RLock()
		status := file.Status
		file.mu.RUnlock()

		if status == FileStatusComplete || status == FileStatusSkipped {
			continue
		}

		wg.Add(1)
		go func(f *FileProgress) {
			defer wg.Done()
			if syncErr := s.SyncFile(mergedCtx, dl, sd, f); syncErr != nil {
				errChan <- fmt.Errorf("file %s: %w", f.Path, syncErr)
			}
		}(file)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	var syncErrors []error
	for err := range errChan {
		s.logger.Error().Err(err).Str("download", sd.ID).Msg("file sync error")
		syncErrors = append(syncErrors, err)
	}

	// Check if all files are complete
	allComplete := true
	for _, f := range sd.Files {
		f.mu.RLock()
		status := f.Status
		f.mu.RUnlock()

		if status != FileStatusComplete && status != FileStatusSkipped {
			allComplete = false
			break
		}
	}

	sd.mu.Lock()
	switch {
	case len(syncErrors) > 0:
		sd.Status = FileStatusError
		sd.Error = fmt.Errorf("sync errors: %v", syncErrors)
	case allComplete:
		sd.Status = FileStatusComplete
		sd.CompletedAt = time.Now()
	default:
		// Some files still pending (not yet complete in downloader)
		sd.Status = FileStatusPending
	}
	sd.mu.Unlock()

	if allComplete {
		s.logger.Info().
			Str("id", sd.ID).
			Str("name", sd.Name).
			Msg("download sync complete")

		if s.onSyncComplete != nil {
			s.onSyncComplete(sd)
		}
	}

	return nil
}

// MoveToFinal moves synced files from staging to final destination.
func (s *Syncer) MoveToFinal(sd *SyncDownload) error {
	s.logger.Info().
		Str("id", sd.ID).
		Str("from", sd.LocalBase).
		Str("to", sd.FinalPath).
		Msg("moving to final destination")

	// Create final directory
	if err := os.MkdirAll(sd.FinalPath, 0750); err != nil {
		return fmt.Errorf("failed to create final directory: %w", err)
	}

	// Move each file
	for _, file := range sd.Files {
		file.mu.RLock()
		status := file.Status
		file.mu.RUnlock()

		if status != FileStatusComplete && status != FileStatusSkipped {
			continue
		}

		finalFilePath := filepath.Join(sd.FinalPath, file.Path)

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(finalFilePath), 0750); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", file.Path, err)
		}

		// Move file
		if renameErr := os.Rename(file.LocalPath, finalFilePath); renameErr != nil {
			// If rename fails (cross-device), try copy+delete
			if copyErr := fileutil.CopyFile(file.LocalPath, finalFilePath); copyErr != nil {
				return fmt.Errorf("failed to move file %s: %w", file.Path, copyErr)
			}
			_ = os.Remove(file.LocalPath)
		}
	}

	// Clean up staging directory
	_ = os.RemoveAll(sd.LocalBase)

	return nil
}

// CancelByKey cancels a sync download by downloader name and download ID.
func (s *Syncer) CancelByKey(downloaderName, downloadID string) error {
	s.downloadsMu.RLock()
	sd, ok := s.downloads[syncKey(downloaderName, downloadID)]
	s.downloadsMu.RUnlock()

	if !ok {
		return nil
	}

	return s.cancelInternal(sd)
}

// cancelInternal performs the actual sync download cancellation.
func (s *Syncer) cancelInternal(sd *SyncDownload) error {
	// Cancel the download's context to stop any ongoing syncing
	sd.Cancel()

	s.logger.Info().
		Str("id", sd.ID).
		Str("name", sd.Name).
		Msg("cancelled download sync")

	// Clean up staging directory
	if sd.LocalBase != "" {
		if err := os.RemoveAll(sd.LocalBase); err != nil {
			s.logger.Warn().
				Err(err).
				Str("path", sd.LocalBase).
				Msg("failed to cleanup staging directory")
		} else {
			s.logger.Debug().
				Str("path", sd.LocalBase).
				Msg("cleaned up staging directory")
		}
	}

	return nil
}

// RemoveByKey removes a sync download from tracking by downloader name and download ID.
func (s *Syncer) RemoveByKey(downloaderName, downloadID string) {
	s.downloadsMu.Lock()
	defer s.downloadsMu.Unlock()
	delete(s.downloads, syncKey(downloaderName, downloadID))
}
