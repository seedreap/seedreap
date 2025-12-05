// Package filesync handles file synchronization from remote to local storage.
package filesync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/download"
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

// SyncJob represents a download sync job.
type SyncJob struct {
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

// GetProgress returns the current progress of the job.
//
//nolint:nonamedreturns // named returns document return values
func (j *SyncJob) GetProgress() (completedSize int64, status FileStatus) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var completed int64
	for _, f := range j.Files {
		transferred, _ := f.Progress()
		completed += transferred
	}

	return completed, j.Status
}

// Cancel cancels the sync job.
func (j *SyncJob) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.cancel != nil {
		j.cancel()
		j.CancelledAt = time.Now()
		j.Status = FileStatusError
		j.Error = context.Canceled
	}
}

// IsCancelled returns true if the job has been cancelled.
func (j *SyncJob) IsCancelled() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return !j.CancelledAt.IsZero()
}

// Context returns the job's context.
func (j *SyncJob) Context() context.Context {
	return j.ctx
}

// UpdateDestination updates the job's final path and category.
// This is used when a download's category changes to another tracked app while syncing.
func (j *SyncJob) UpdateDestination(finalPath, category string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.FinalPath = finalPath
	j.Category = category
}

// GetFinalPath returns the job's final path.
func (j *SyncJob) GetFinalPath() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.FinalPath
}

// SyncJobSnapshot is a point-in-time snapshot of a sync job.
type SyncJobSnapshot struct {
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
	BytesPerSec   int64 // Job-level transfer speed
}

// Snapshot returns a point-in-time snapshot of the job.
func (j *SyncJob) Snapshot() SyncJobSnapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()

	files := make([]FileProgressSnapshot, len(j.Files))
	var completedSize int64
	var bytesPerSec int64
	for i, f := range j.Files {
		files[i] = f.Snapshot()
		completedSize += files[i].Transferred
		// Sum speed from actively syncing files (reported by transfer backend)
		if files[i].Status == FileStatusSyncing {
			bytesPerSec += files[i].BytesPerSec
		}
	}

	return SyncJobSnapshot{
		ID:            j.ID,
		Name:          j.Name,
		Downloader:    j.Downloader,
		Category:      j.Category,
		RemoteBase:    j.RemoteBase,
		LocalBase:     j.LocalBase,
		FinalPath:     j.FinalPath,
		TotalSize:     j.TotalSize,
		TotalFiles:    j.TotalFiles,
		CompletedSize: completedSize,
		Status:        j.Status,
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

	jobs      map[string]*SyncJob
	jobsMu    sync.RWMutex
	semaphore chan struct{}

	// Speed history for UI sparkline (last 5 minutes)
	speedHistory   []SpeedSample
	speedHistoryMu sync.RWMutex

	// Callbacks
	onJobComplete  func(job *SyncJob)
	onFileComplete func(job *SyncJob, file *FileProgress)
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

// WithOnJobComplete sets a callback for when a job completes.
func WithOnJobComplete(fn func(job *SyncJob)) Option {
	return func(s *Syncer) {
		s.onJobComplete = fn
	}
}

// WithOnFileComplete sets a callback for when a file completes.
func WithOnFileComplete(fn func(job *SyncJob, file *FileProgress)) Option {
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
		jobs:          make(map[string]*SyncJob),
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

// CreateJob creates a new sync job for a download.
func (s *Syncer) CreateJob(dl *download.Download, downloaderName, finalPath string) *SyncJob {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	// Check if job already exists
	if job, ok := s.jobs[dl.ID]; ok {
		return job
	}

	// Create per-job context for cancellation
	ctx, cancel := context.WithCancel(context.Background())

	job := &SyncJob{
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

		fp := &FileProgress{
			Path:       f.Path,
			RemotePath: filepath.Join(dl.SavePath, f.Path),
			LocalPath:  filepath.Join(job.LocalBase, f.Path),
			Size:       f.Size,
			Status:     FileStatusPending,
		}

		// Check if file already exists at final destination with correct size
		// This handles the case where a restart happens after sync but before cleanup
		// Note: f.Path already includes the torrent name as the first component
		finalFilePath := filepath.Join(finalPath, f.Path)
		if info, err := os.Stat(finalFilePath); err == nil && info.Size() == f.Size {
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

		job.Files = append(job.Files, fp)
		job.TotalSize += f.Size
		job.TotalFiles++
	}

	s.jobs[dl.ID] = job
	return job
}

// GetJob returns a job by ID.
func (s *Syncer) GetJob(id string) (*SyncJob, bool) {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

// GetAllJobs returns all jobs.
func (s *Syncer) GetAllJobs() []*SyncJob {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()

	jobs := make([]*SyncJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
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
func (s *Syncer) SyncFile(ctx context.Context, _ download.Downloader, job *SyncJob, file *FileProgress) error {
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
		Str("job", job.ID).
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
		Str("job", job.ID).
		Str("file", file.Path).
		Int64("size", file.Size).
		Int64("bps", file.BytesPerSec).
		Msg("file sync complete")

	// Trigger callback
	if s.onFileComplete != nil {
		s.onFileComplete(job, file)
	}

	return nil
}

// SyncJob syncs all ready files in a job.
//
//nolint:gocognit,funlen // sync logic requires multiple phases and error handling paths
func (s *Syncer) SyncJob(ctx context.Context, dl download.Downloader, job *SyncJob) error {
	// Check if job is already cancelled
	if job.IsCancelled() {
		return context.Canceled
	}

	// Use the job's context for cancellation, but also respect the parent context
	// Create a merged context that cancels if either is cancelled
	jobCtx := job.Context()
	mergedCtx, mergedCancel := context.WithCancel(ctx)
	defer mergedCancel()

	// Watch for job cancellation
	go func() {
		select {
		case <-jobCtx.Done():
			mergedCancel()
		case <-mergedCtx.Done():
		}
	}()

	job.mu.Lock()
	job.Status = FileStatusSyncing
	job.StartedAt = time.Now()
	job.mu.Unlock()

	backendName := "unknown"
	if s.transferer != nil {
		backendName = s.transferer.Name()
	}

	s.logger.Info().
		Str("id", job.ID).
		Str("name", job.Name).
		Int("files", job.TotalFiles).
		Str("backend", backendName).
		Msg("starting job sync")

	// Get current file states from downloader
	files, err := dl.GetFiles(mergedCtx, job.ID)
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
	errChan := make(chan error, len(job.Files))

	for _, file := range job.Files {
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
			if syncErr := s.SyncFile(mergedCtx, dl, job, f); syncErr != nil {
				errChan <- fmt.Errorf("file %s: %w", f.Path, syncErr)
			}
		}(file)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	var syncErrors []error
	for err := range errChan {
		s.logger.Error().Err(err).Str("job", job.ID).Msg("file sync error")
		syncErrors = append(syncErrors, err)
	}

	// Check if all files are complete
	allComplete := true
	for _, f := range job.Files {
		f.mu.RLock()
		status := f.Status
		f.mu.RUnlock()

		if status != FileStatusComplete && status != FileStatusSkipped {
			allComplete = false
			break
		}
	}

	job.mu.Lock()
	switch {
	case len(syncErrors) > 0:
		job.Status = FileStatusError
		job.Error = fmt.Errorf("sync errors: %v", syncErrors)
	case allComplete:
		job.Status = FileStatusComplete
		job.CompletedAt = time.Now()
	default:
		// Some files still pending (not yet complete in downloader)
		job.Status = FileStatusPending
	}
	job.mu.Unlock()

	if allComplete {
		s.logger.Info().
			Str("id", job.ID).
			Str("name", job.Name).
			Msg("job sync complete")

		if s.onJobComplete != nil {
			s.onJobComplete(job)
		}
	}

	return nil
}

// MoveToFinal moves synced files from staging to final destination.
func (s *Syncer) MoveToFinal(job *SyncJob) error {
	s.logger.Info().
		Str("id", job.ID).
		Str("from", job.LocalBase).
		Str("to", job.FinalPath).
		Msg("moving to final destination")

	// Create final directory
	if err := os.MkdirAll(job.FinalPath, 0750); err != nil {
		return fmt.Errorf("failed to create final directory: %w", err)
	}

	// Move each file
	for _, file := range job.Files {
		file.mu.RLock()
		status := file.Status
		file.mu.RUnlock()

		if status != FileStatusComplete && status != FileStatusSkipped {
			continue
		}

		// Calculate final path
		// Note: file.Path already includes the torrent name as the first component
		finalFilePath := filepath.Join(job.FinalPath, file.Path)

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(finalFilePath), 0750); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", file.Path, err)
		}

		// Move file
		if renameErr := os.Rename(file.LocalPath, finalFilePath); renameErr != nil {
			// If rename fails (cross-device), try copy+delete
			if copyErr := copyFile(file.LocalPath, finalFilePath); copyErr != nil {
				return fmt.Errorf("failed to move file %s: %w", file.Path, copyErr)
			}
			_ = os.Remove(file.LocalPath)
		}
	}

	// Clean up staging directory
	_ = os.RemoveAll(job.LocalBase)

	return nil
}

// CancelJob cancels a sync job and cleans up its staging files.
func (s *Syncer) CancelJob(id string) error {
	s.jobsMu.Lock()
	job, ok := s.jobs[id]
	s.jobsMu.Unlock()

	if !ok {
		return nil
	}

	// Cancel the job's context to stop any ongoing syncing
	job.Cancel()

	s.logger.Info().
		Str("id", id).
		Str("name", job.Name).
		Msg("cancelled sync job")

	// Clean up staging directory
	if job.LocalBase != "" {
		if err := os.RemoveAll(job.LocalBase); err != nil {
			s.logger.Warn().
				Err(err).
				Str("path", job.LocalBase).
				Msg("failed to cleanup staging directory")
		} else {
			s.logger.Debug().
				Str("path", job.LocalBase).
				Msg("cleaned up staging directory")
		}
	}

	return nil
}

// RemoveJob removes a job from tracking.
func (s *Syncer) RemoveJob(id string) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	delete(s.jobs, id)
}

func copyFile(src, dst string) (retErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dstFile.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
