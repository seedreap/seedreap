// Package apitypes provides API response types for the SeedReap HTTP API.
package apitypes

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Stats represents orchestrator statistics.
type Stats struct {
	TotalTracked         int            `json:"total_tracked"`
	ByState              map[string]int `json:"by_state,omitempty"`
	DownloadingOnSeedbox int            `json:"downloading_on_seedbox,omitempty"`
	PausedOnSeedbox      int            `json:"paused_on_seedbox,omitempty"`
}

// Download represents a comprehensive view of a tracked download.
// Includes information from DownloadJob, SyncJob, MoveJob, and AppJob.
type Download struct {
	// Core identification
	ID         string `json:"id"`
	Name       string `json:"name"`
	Downloader string `json:"downloader"`
	Category   string `json:"category,omitempty"`
	App        string `json:"app,omitempty"`

	// High-level state (computed from all job states)
	State string `json:"state"`
	Error string `json:"error,omitempty"`

	// Download job info (from seedbox)
	DownloadJob *DownloadJobInfo `json:"download_job,omitempty"`

	// Sync job info (file transfers)
	SyncJob *SyncJobInfo `json:"sync_job,omitempty"`

	// Move job info
	MoveJob *MoveJobInfo `json:"move_job,omitempty"`

	// App jobs info (may have multiple apps)
	AppJobs []AppJobInfo `json:"app_jobs,omitempty"`

	// Aggregate sync transfer info (for convenience - populated from active syncer)
	TotalSize     int64  `json:"total_size,omitempty"`
	CompletedSize int64  `json:"completed_size,omitempty"`
	TotalFiles    int    `json:"total_files,omitempty"`
	BytesPerSec   int64  `json:"bytes_per_sec,omitempty"`
	Files         []File `json:"files,omitempty"`

	// Timestamps
	DiscoveredAt string `json:"discovered_at,omitempty"`
}

// DownloadJobInfo contains information about the download job on the seedbox.
type DownloadJobInfo struct {
	Status        string  `json:"status"`         // downloading, paused, complete, error
	Progress      float64 `json:"progress"`       // 0.0 to 1.0
	Size          int64   `json:"size"`           // Total size in bytes
	Downloaded    int64   `json:"downloaded"`     // Bytes downloaded on seedbox
	DownloadSpeed int64   `json:"download_speed"` // Current download speed (seedbox)
	SavePath      string  `json:"save_path,omitempty"`
	CompletedAt   string  `json:"completed_at,omitempty"`
	Error         string  `json:"error,omitempty"`
}

// SyncJobInfo contains information about the sync job (file transfers from seedbox).
type SyncJobInfo struct {
	ID          string `json:"id"`
	Status      string `json:"status"` // pending, syncing, complete, error, cancelled
	RemoteBase  string `json:"remote_base,omitempty"`
	LocalBase   string `json:"local_base,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

// MoveJobInfo contains information about the move job (from staging to final location).
type MoveJobInfo struct {
	ID              string `json:"id"`
	Status          string `json:"status"` // pending, moving, complete, error
	SourcePath      string `json:"source_path,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	CompletedAt     string `json:"completed_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

// AppJobInfo contains information about an app notification job.
type AppJobInfo struct {
	ID          string `json:"id"`
	AppName     string `json:"app_name"`
	Status      string `json:"status"` // pending, processing, complete, error
	Path        string `json:"path,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

// File represents a file within a download, combining DownloadFile and SyncFile info.
type File struct {
	// Common fields
	Path string `json:"path"`
	Size int64  `json:"size"`

	// Download file info (from seedbox)
	Downloaded       int64   `json:"downloaded"`        // Bytes downloaded on seedbox
	DownloadProgress float64 `json:"download_progress"` // 0.0 to 1.0
	Priority         int     `json:"priority"`          // Download priority (0 = not selected)

	// Sync file info (local transfer)
	SyncedSize int64  `json:"synced_size"`           // Bytes transferred locally
	SyncStatus string `json:"sync_status,omitempty"` // pending, syncing, complete, error, cancelled
	SyncError  string `json:"sync_error,omitempty"`

	// Live transfer info (from active syncer)
	BytesPerSec int64 `json:"bytes_per_sec,omitempty"`
}

// DownloadClient represents a configured download client.
type DownloadClient struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// App represents a configured application.
type App struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Category string `json:"category,omitempty"`
}

// SpeedSample represents a speed measurement at a point in time.
type SpeedSample struct {
	Speed     int64 `json:"speed"`
	Timestamp int64 `json:"timestamp"`
}
