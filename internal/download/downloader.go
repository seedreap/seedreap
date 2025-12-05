// Package download provides interfaces and implementations for download clients.
package download

import (
	"context"
	"io"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/config"
)

// configurable is implemented by all downloaders to support shared options.
type configurable interface {
	setLogger(zerolog.Logger)
}

// Option is a functional option for configuring downloaders.
type Option func(configurable)

// WithLogger sets the logger for any downloader.
func WithLogger(logger zerolog.Logger) Option {
	return func(c configurable) {
		c.setLogger(logger)
	}
}

// FileState represents the download state of a file.
type FileState string

const (
	// FileStateDownloading indicates the file is still being downloaded.
	FileStateDownloading FileState = "downloading"
	// FileStateComplete indicates the file has finished downloading.
	FileStateComplete FileState = "complete"
)

// TorrentState represents the overall state of a torrent/download.
type TorrentState string

const (
	// TorrentStateDownloading indicates the torrent is still downloading.
	TorrentStateDownloading TorrentState = "downloading"
	// TorrentStateComplete indicates all files in the torrent are complete.
	TorrentStateComplete TorrentState = "complete"
	// TorrentStatePaused indicates the torrent is paused.
	TorrentStatePaused TorrentState = "paused"
	// TorrentStateError indicates an error state.
	TorrentStateError TorrentState = "error"
)

// File represents a single file within a download.
type File struct {
	// Path is the relative path of the file within the download.
	Path string
	// Size is the total size in bytes.
	Size int64
	// Downloaded is the number of bytes downloaded.
	Downloaded int64
	// State is the current download state.
	State FileState
	// Priority indicates if the file is selected for download (0 = skip).
	Priority int
}

// Download represents a download item (torrent, nzb, etc).
type Download struct {
	// ID is the unique identifier for the download.
	ID string
	// Name is the display name of the download.
	Name string
	// Hash is the info hash (for torrents).
	Hash string
	// Category is the category/label assigned to this download.
	Category string
	// State is the overall download state.
	State TorrentState
	// SavePath is the path where files are saved on the remote system.
	SavePath string
	// ContentPath is the full path to the content (file or directory).
	ContentPath string
	// Size is the total size in bytes.
	Size int64
	// Downloaded is the number of bytes downloaded.
	Downloaded int64
	// Progress is the download progress (0.0 to 1.0).
	Progress float64
	// Files is the list of files in this download.
	Files []File
	// AddedOn is when the download was added.
	AddedOn time.Time
	// CompletedOn is when the download completed (zero if not complete).
	CompletedOn time.Time
}

// Downloader is the interface that download clients must implement.
type Downloader interface {
	// Name returns the configured name of this downloader instance.
	Name() string

	// Type returns the type of downloader (e.g., "qbittorrent", "sabnzbd").
	Type() string

	// Connect establishes a connection to the download client.
	Connect(ctx context.Context) error

	// Close closes the connection to the download client.
	Close() error

	// ListDownloads returns all downloads matching the given categories.
	// If categories is empty, returns all downloads.
	ListDownloads(ctx context.Context, categories []string) ([]Download, error)

	// GetDownload returns a specific download by ID/hash.
	GetDownload(ctx context.Context, id string) (*Download, error)

	// GetFiles returns the list of files for a download with current state.
	GetFiles(ctx context.Context, id string) ([]File, error)

	// OpenFile opens a remote file for reading via SFTP/SSH.
	OpenFile(ctx context.Context, path string) (io.ReadCloser, error)

	// SSHConfig returns the SSH configuration for this downloader.
	SSHConfig() config.SSHConfig
}

// Registry holds all configured downloaders.
type Registry struct {
	downloaders map[string]Downloader
}

// NewRegistry creates a new downloader registry.
func NewRegistry() *Registry {
	return &Registry{
		downloaders: make(map[string]Downloader),
	}
}

// Register adds a downloader to the registry.
func (r *Registry) Register(name string, d Downloader) {
	r.downloaders[name] = d
}

// Get returns a downloader by name.
func (r *Registry) Get(name string) (Downloader, bool) {
	d, ok := r.downloaders[name]
	return d, ok
}

// All returns all registered downloaders.
func (r *Registry) All() map[string]Downloader {
	return r.downloaders
}
