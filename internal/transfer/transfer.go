// Package transfer provides interfaces and implementations for file transfer backends.
package transfer

import (
	"context"

	"github.com/rs/zerolog"
)

// configurable is implemented by all transferers to support shared options.
type configurable interface {
	setLogger(zerolog.Logger)
}

// Option is a functional option for configuring transferers.
type Option func(configurable)

// WithLogger sets the logger for any transferer.
func WithLogger(logger zerolog.Logger) Option {
	return func(c configurable) {
		c.setLogger(logger)
	}
}

// Backend represents a file transfer backend type.
type Backend string

const (
	// BackendRclone uses rclone for file transfers.
	BackendRclone Backend = "rclone"
)

// SSHConfig holds SSH connection configuration for transfer backends.
type SSHConfig struct {
	Host           string
	Port           int
	User           string
	KeyFile        string
	KnownHostsFile string // Path to known_hosts file (empty if IgnoreHostKey is true)
	IgnoreHostKey  bool   // Skip host key verification
}

// Options holds configuration for creating a Transferer.
type Options struct {
	// SSH configuration for remote connections
	SSH SSHConfig

	// ParallelConnections is the number of parallel connections/streams per file
	ParallelConnections int

	// SpeedLimit in bytes per second (0 = unlimited)
	SpeedLimit int64
}

// Request represents a single file transfer request.
type Request struct {
	// RemotePath is the full path to the file on the remote server
	RemotePath string

	// LocalPath is the full path where the file should be saved locally
	LocalPath string

	// Size is the expected size of the file in bytes
	Size int64
}

// Progress represents the current progress of a transfer.
type Progress struct {
	// Transferred is the number of bytes transferred so far
	Transferred int64

	// BytesPerSec is the current transfer speed
	BytesPerSec int64
}

// ProgressFunc is a callback function for progress updates.
type ProgressFunc func(Progress)

// Transferer is the interface for file transfer backends.
type Transferer interface {
	// Transfer copies a file from remote to local.
	// The onProgress callback is called periodically with transfer progress.
	// The transfer should be cancellable via context.
	Transfer(ctx context.Context, req Request, onProgress ProgressFunc) error

	// Name returns the name of the transfer backend.
	Name() string

	// GetSpeed returns the current aggregate transfer speed in bytes per second.
	// This is the total speed across all active transfers.
	GetSpeed() int64

	// PrepareShutdown is called before context cancellation to allow the backend
	// to suppress expected error messages during graceful shutdown.
	PrepareShutdown()

	// Close releases any resources held by the transferer.
	Close() error
}
