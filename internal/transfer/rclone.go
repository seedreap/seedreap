package transfer

import (
	"context"
	"fmt"
	"io"
	"log" //nolint:depguard // needed to suppress rclone's internal error logging during shutdown
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rs/zerolog"

	// Import backends we need.
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/sftp"
)

// Default rclone configuration values.
const (
	rcloneDefaultParallelConnections = 8
	rcloneDefaultSSHPort             = 22
	rcloneDefaultProgressInterval    = 500 * time.Millisecond
	rcloneDefaultChunkSize           = "64k"           // SFTP chunk size
	rcloneBytesPerMB                 = 1 << 20         // 1024 * 1024
	rcloneMinChunkSize               = 10 * bytesPerMB // Don't split files if chunks would be under 10MB
)

// bytesPerMB is shared constant for MB calculations.
const bytesPerMB = 1 << 20

// rcloneGlobalsOnce ensures global rclone configuration is only set once.
// This prevents race conditions when multiple transferers are created concurrently.
//
//nolint:gochecknoglobals // sync primitives for thread-safe rclone initialization
var rcloneGlobalsOnce sync.Once

// rcloneNewFsMu serializes fs.NewFs calls to work around race conditions in rclone's
// config loading (github.com/rclone/rclone/issues/8666). This is only needed during filesystem creation.
//
//nolint:gochecknoglobals // sync primitives for thread-safe rclone initialization
var rcloneNewFsMu sync.Mutex

// rcloneTransferer implements Transferer using rclone.
// It is private and only exposed via the Transferer interface.
type rcloneTransferer struct {
	ssh                 SSHConfig
	parallelConnections int
	speedLimit          int64
	logger              zerolog.Logger

	// Cached SFTP filesystem to reuse connections
	sftpFs   fs.Fs
	sftpOnce sync.Once
	sftpErr  error
}

// setLogger implements configurable for shared options.
func (t *rcloneTransferer) setLogger(logger zerolog.Logger) {
	t.logger = logger
}

// NewRclone creates a new rclone transferer and returns it as Transferer.
func NewRclone(opts Options, options ...Option) Transferer {
	parallelConnections := opts.ParallelConnections
	if parallelConnections == 0 {
		parallelConnections = rcloneDefaultParallelConnections
	}

	sshPort := opts.SSH.Port
	if sshPort == 0 {
		sshPort = rcloneDefaultSSHPort
	}

	t := &rcloneTransferer{
		ssh: SSHConfig{
			Host:           opts.SSH.Host,
			Port:           sshPort,
			User:           opts.SSH.User,
			KeyFile:        opts.SSH.KeyFile,
			KnownHostsFile: opts.SSH.KnownHostsFile,
			IgnoreHostKey:  opts.SSH.IgnoreHostKey,
		},
		parallelConnections: parallelConnections,
		speedLimit:          opts.SpeedLimit,
		logger:              zerolog.Nop(),
	}

	for _, opt := range options {
		opt(t)
	}

	// Configure global rclone settings
	t.configureGlobals()

	return t
}

// configureGlobals sets up global rclone configuration.
// Uses sync.Once to ensure configuration happens only once, preventing race conditions
// when multiple transferers are created concurrently.
func (t *rcloneTransferer) configureGlobals() {
	rcloneGlobalsOnce.Do(func() {
		// Set up global config
		ci := fs.GetConfig(context.Background())

		// Set transfer concurrency
		ci.Transfers = 1 // We handle concurrency at the syncer level
		ci.Checkers = 1  // Minimal checking
		ci.MultiThreadStreams = t.parallelConnections

		// Only use multi-thread downloads for files large enough that each chunk is at least minChunkSize
		// e.g., with 8 connections and 10MB min chunk, files must be at least 80MB to split
		ci.MultiThreadCutoff = fs.SizeSuffix(t.parallelConnections * rcloneMinChunkSize)
		ci.StreamingUploadCutoff = 0 // Always stream

		// Set bandwidth limit if configured (applies to both upload and download)
		if t.speedLimit > 0 {
			ci.BwLimit = fs.BwTimetable{
				{Bandwidth: fs.BwPair{
					Tx: fs.SizeSuffix(t.speedLimit),
					Rx: fs.SizeSuffix(t.speedLimit),
				}},
			}
		}

		// Reduce verbosity
		ci.LogLevel = fs.LogLevelError
	})
}

// Name returns the name of the transfer backend.
func (t *rcloneTransferer) Name() string {
	return string(BackendRclone)
}

// PrepareShutdown suppresses rclone error logging during shutdown.
// Call this before cancelling contexts to avoid noisy "context canceled" errors.
func (t *rcloneTransferer) PrepareShutdown() {
	// Suppress standard library log output (used by rclone for some errors)
	log.SetOutput(io.Discard)

	// Set rclone log level to suppress error messages
	ci := fs.GetConfig(context.Background())
	ci.LogLevel = fs.LogLevelEmergency
}

// Close releases any resources held by the transferer.
func (t *rcloneTransferer) Close() error {
	// Close SFTP connection if we have one
	if t.sftpFs != nil {
		if shutdowner, ok := t.sftpFs.(fs.Shutdowner); ok {
			_ = shutdowner.Shutdown(context.Background())
		}
	}
	return nil
}

// GetSpeed returns the current aggregate transfer speed from rclone's global stats.
func (t *rcloneTransferer) GetSpeed() int64 {
	stats, err := accounting.GlobalStats().RemoteStats(true)
	if err != nil {
		return 0
	}
	if speed, ok := stats["speed"].(float64); ok {
		return int64(speed)
	}
	return 0
}

// getSFTPFs returns a cached SFTP filesystem or creates a new one.
func (t *rcloneTransferer) getSFTPFs(ctx context.Context) (fs.Fs, error) {
	t.sftpOnce.Do(func() {
		t.sftpFs, t.sftpErr = t.createSFTPFs(ctx)
	})
	return t.sftpFs, t.sftpErr
}

// createSFTPFs creates a new SFTP filesystem.
func (t *rcloneTransferer) createSFTPFs(ctx context.Context) (fs.Fs, error) {
	// Build connection string using rclone's backend connection string format.
	// Using fs.NewFs with a connection string ensures all defaults are applied properly.
	// Format: :sftp,option=value,option2=value2:/path
	// See: https://github.com/rclone/rclone/issues/8666
	//
	// Note: If known_hosts_file is not set, rclone uses ssh.InsecureIgnoreHostKey()
	// which allows any host key. Only set it when we have an explicit file.
	knownHostsOpt := ""
	if !t.ssh.IgnoreHostKey && t.ssh.KnownHostsFile != "" {
		knownHostsOpt = fmt.Sprintf(",known_hosts_file=%s", t.ssh.KnownHostsFile)
	}

	connStr := fmt.Sprintf(
		":sftp,host=%s,port=%d,user=%s,key_file=%s%s,"+
			"concurrency=%d,chunk_size=%s,disable_hashcheck=true,"+
			"set_modtime=false,skip_links=true,shell_type=none:/",
		t.ssh.Host,
		t.ssh.Port,
		t.ssh.User,
		t.ssh.KeyFile,
		knownHostsOpt,
		t.parallelConnections,
		rcloneDefaultChunkSize,
	)

	// Serialize fs.NewFs calls to work around race conditions in rclone's config loading
	rcloneNewFsMu.Lock()
	sftpFs, err := fs.NewFs(ctx, connStr)
	rcloneNewFsMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to create sftp filesystem: %w", err)
	}

	t.logger.Info().
		Str("host", t.ssh.Host).
		Int("port", t.ssh.Port).
		Str("user", t.ssh.User).
		Int("concurrency", t.parallelConnections).
		Msg("rclone SFTP connection established")

	return sftpFs, nil
}

// Transfer copies a file from remote to local using rclone.
func (t *rcloneTransferer) Transfer(ctx context.Context, req Request, onProgress ProgressFunc) error {
	t.logger.Debug().
		Str("remote", req.RemotePath).
		Str("local", req.LocalPath).
		Int64("size", req.Size).
		Msg("starting rclone transfer")

	// Get or create SFTP filesystem
	sftpFs, err := t.getSFTPFs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sftp filesystem: %w", err)
	}

	// Create local filesystem for destination directory
	localDir := filepath.Dir(req.LocalPath)
	if mkdirErr := os.MkdirAll(localDir, 0750); mkdirErr != nil {
		return fmt.Errorf("failed to create local directory: %w", mkdirErr)
	}

	// Serialize fs.NewFs calls to work around race conditions in rclone's config loading
	rcloneNewFsMu.Lock()
	localFs, err := fs.NewFs(ctx, localDir)
	rcloneNewFsMu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to create local filesystem: %w", err)
	}

	// Get the remote file object from our configured SFTP filesystem
	// RemotePath is absolute (starts with /), but sftpFs is rooted at /
	// so we need to strip the leading slash
	remotePath := req.RemotePath
	if len(remotePath) > 0 && remotePath[0] == '/' {
		remotePath = remotePath[1:]
	}

	srcObj, err := sftpFs.NewObject(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("failed to get remote file %q: %w", req.RemotePath, err)
	}

	return t.copyWithProgress(ctx, localFs, srcObj, filepath.Base(req.LocalPath), onProgress)
}

// copyWithProgress copies a file and reports progress using per-transfer stats.
func (t *rcloneTransferer) copyWithProgress(
	ctx context.Context,
	dstFs fs.Fs,
	srcObj fs.Object,
	dstFileName string,
	onProgress ProgressFunc,
) error {
	// Create a unique stats group for this transfer to avoid conflicts with concurrent transfers
	// See: https://github.com/rclone/rclone/blob/master/fs/accounting/stats_groups.go
	groupName := fmt.Sprintf("transfer-%s-%d", dstFileName, time.Now().UnixNano())
	transferCtx := accounting.WithStatsGroup(ctx, groupName)
	stats := accounting.StatsGroup(transferCtx, groupName)

	// Start progress monitoring
	var wg sync.WaitGroup
	done := make(chan struct{})
	startTime := time.Now()

	if onProgress != nil {
		wg.Go(func() {
			t.monitorProgress(stats, onProgress, done)
		})
	}

	// Perform the copy with the transfer-specific context
	_, err := operations.Copy(transferCtx, dstFs, nil, dstFileName, srcObj)

	// Signal progress monitor to stop
	close(done)
	wg.Wait()

	if err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	// Calculate final speed
	elapsed := time.Since(startTime).Seconds()
	var speed int64
	if elapsed > 0 {
		speed = int64(float64(srcObj.Size()) / elapsed)
	}

	// Send final progress update
	if onProgress != nil {
		onProgress(Progress{
			Transferred: srcObj.Size(),
			BytesPerSec: speed,
		})
	}

	t.logger.Debug().
		Str("file", srcObj.Remote()).
		Int64("size", srcObj.Size()).
		Float64("speed_mbps", float64(speed)/rcloneBytesPerMB).
		Msg("rclone transfer complete")

	return nil
}

// monitorProgress periodically reports transfer progress from the stats group.
func (t *rcloneTransferer) monitorProgress(
	stats *accounting.StatsInfo,
	onProgress ProgressFunc,
	done chan struct{},
) {
	ticker := time.NewTicker(rcloneDefaultProgressInterval)
	defer ticker.Stop()

	var lastBytes int64
	var lastTime time.Time

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			now := time.Now()
			bytes := stats.GetBytes()

			var speed int64
			if !lastTime.IsZero() && bytes > lastBytes {
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed > 0 {
					speed = int64(float64(bytes-lastBytes) / elapsed)
				}
			}
			lastBytes = bytes
			lastTime = now

			onProgress(Progress{
				Transferred: bytes,
				BytesPerSec: speed,
			})
		}
	}
}
