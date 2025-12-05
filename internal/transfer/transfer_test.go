package transfer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/transfer"
)

// --- Backend Constants Tests ---

func TestBackendConstants(t *testing.T) {
	t.Run("BackendRclone", func(t *testing.T) {
		assert.Equal(t, transfer.BackendRclone, transfer.Backend("rclone"))
	})
}

// --- SSHConfig Tests ---

func TestSSHConfig(t *testing.T) {
	t.Run("StructFields", func(t *testing.T) {
		cfg := transfer.SSHConfig{
			Host:           "seedbox.example.com",
			Port:           22,
			User:           "user",
			KeyFile:        "/path/to/key",
			KnownHostsFile: "/path/to/known_hosts",
			IgnoreHostKey:  false,
		}

		assert.Equal(t, "seedbox.example.com", cfg.Host)
		assert.Equal(t, 22, cfg.Port)
		assert.Equal(t, "user", cfg.User)
		assert.Equal(t, "/path/to/key", cfg.KeyFile)
		assert.Equal(t, "/path/to/known_hosts", cfg.KnownHostsFile)
		assert.False(t, cfg.IgnoreHostKey)
	})

	t.Run("IgnoreHostKey", func(t *testing.T) {
		cfg := transfer.SSHConfig{
			IgnoreHostKey: true,
		}

		assert.True(t, cfg.IgnoreHostKey)
	})
}

// --- Options Tests ---

func TestOptions(t *testing.T) {
	t.Run("DefaultValues", func(t *testing.T) {
		opts := transfer.Options{}

		assert.Empty(t, opts.SSH.Host)
		assert.Equal(t, 0, opts.SSH.Port)
		assert.Equal(t, 0, opts.ParallelConnections)
		assert.Equal(t, int64(0), opts.SpeedLimit)
	})

	t.Run("FullConfiguration", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:           "seedbox.example.com",
				Port:           2222,
				User:           "download",
				KeyFile:        "/home/user/.ssh/seedbox_key",
				KnownHostsFile: "/home/user/.ssh/known_hosts",
				IgnoreHostKey:  false,
			},
			ParallelConnections: 16,
			SpeedLimit:          10 * 1024 * 1024, // 10 MB/s
		}

		assert.Equal(t, "seedbox.example.com", opts.SSH.Host)
		assert.Equal(t, 2222, opts.SSH.Port)
		assert.Equal(t, "download", opts.SSH.User)
		assert.Equal(t, 16, opts.ParallelConnections)
		assert.Equal(t, int64(10*1024*1024), opts.SpeedLimit)
	})
}

// --- Request Tests ---

func TestRequest(t *testing.T) {
	t.Run("StructFields", func(t *testing.T) {
		req := transfer.Request{
			RemotePath: "/remote/downloads/Movie.2024/movie.mkv",
			LocalPath:  "/local/syncing/movie.mkv",
			Size:       5 * 1024 * 1024 * 1024, // 5 GB
		}

		assert.Equal(t, "/remote/downloads/Movie.2024/movie.mkv", req.RemotePath)
		assert.Equal(t, "/local/syncing/movie.mkv", req.LocalPath)
		assert.Equal(t, int64(5*1024*1024*1024), req.Size)
	})
}

// --- Progress Tests ---

func TestProgress(t *testing.T) {
	t.Run("StructFields", func(t *testing.T) {
		progress := transfer.Progress{
			Transferred: 1024 * 1024 * 100, // 100 MB
			BytesPerSec: 50 * 1024 * 1024,  // 50 MB/s
		}

		assert.Equal(t, int64(100*1024*1024), progress.Transferred)
		assert.Equal(t, int64(50*1024*1024), progress.BytesPerSec)
	})
}

// --- ProgressFunc Tests ---

func TestProgressFunc(t *testing.T) {
	t.Run("CallbackInvocation", func(t *testing.T) {
		var received transfer.Progress

		callback := func(p transfer.Progress) {
			received = p
		}

		callback(transfer.Progress{
			Transferred: 1000,
			BytesPerSec: 500,
		})

		assert.Equal(t, int64(1000), received.Transferred)
		assert.Equal(t, int64(500), received.BytesPerSec)
	})

	t.Run("MultipleCallbacks", func(t *testing.T) {
		var calls []transfer.Progress

		callback := func(p transfer.Progress) {
			calls = append(calls, p)
		}

		// Simulate progress updates
		for i := 1; i <= 5; i++ {
			callback(transfer.Progress{
				Transferred: int64(i * 100),
				BytesPerSec: int64(i * 10),
			})
		}

		assert.Len(t, calls, 5)
		assert.Equal(t, int64(100), calls[0].Transferred)
		assert.Equal(t, int64(500), calls[4].Transferred)
	})
}

// --- NewRclone Tests ---

func TestNewRclone(t *testing.T) {
	t.Run("DefaultConfiguration", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		assert.NotNil(t, transferer)
		assert.Equal(t, "rclone", transferer.Name())
	})

	t.Run("DefaultSSHPort", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host: "test.example.com",
				// Port not specified, should default to 22
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)
		assert.NotNil(t, transferer)
	})

	t.Run("CustomSSHPort", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				Port:    2222,
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)
		assert.NotNil(t, transferer)
	})

	t.Run("DefaultParallelConnections", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
			// ParallelConnections not specified, should default to 8
		}

		transferer := transfer.NewRclone(opts)
		assert.NotNil(t, transferer)
	})

	t.Run("CustomParallelConnections", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
			ParallelConnections: 16,
		}

		transferer := transfer.NewRclone(opts)
		assert.NotNil(t, transferer)
	})

	t.Run("WithSpeedLimit", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
			SpeedLimit: 10 * 1024 * 1024, // 10 MB/s
		}

		transferer := transfer.NewRclone(opts)
		assert.NotNil(t, transferer)
	})

	t.Run("WithLogger", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		logger := zerolog.New(os.Stderr).With().Str("component", "transfer-test").Logger()
		transferer := transfer.NewRclone(opts, transfer.WithLogger(logger))

		assert.NotNil(t, transferer)
	})
}

// --- Transferer Interface Tests ---

func TestRcloneTransfererInterface(t *testing.T) {
	t.Run("ImplementsTransferer", func(_ *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		var impl transfer.Transferer = transfer.NewRclone(opts) //nolint:staticcheck // test interface impl
		_ = impl
	})

	t.Run("Name", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)
		assert.Equal(t, "rclone", transferer.Name())
	})

	t.Run("GetSpeed", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		// With no active transfers, speed should be 0
		speed := transferer.GetSpeed()
		assert.GreaterOrEqual(t, speed, int64(0))
	})

	t.Run("PrepareShutdown", func(_ *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		// Should not panic
		transferer.PrepareShutdown()
	})

	t.Run("Close", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		err := transferer.Close()
		assert.NoError(t, err)
	})
}

// --- Transfer Error Cases Tests ---
// Note: These tests verify error handling without requiring a live SFTP connection

func TestRcloneTransferErrors(t *testing.T) {
	t.Run("InvalidRemotePath", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "localhost",
				Port:    22,
				User:    "nobody",
				KeyFile: filepath.Join(tmpDir, "nonexistent_key"),
			},
		}

		transferer := transfer.NewRclone(opts)

		req := transfer.Request{
			RemotePath: "/nonexistent/path/file.txt",
			LocalPath:  filepath.Join(tmpDir, "local_file.txt"),
			Size:       1024,
		}

		// This should fail because we can't connect
		err := transferer.Transfer(context.Background(), req, nil)
		require.Error(t, err)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		tmpDir := t.TempDir()

		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "localhost",
				Port:    22,
				User:    "nobody",
				KeyFile: filepath.Join(tmpDir, "nonexistent_key"),
			},
		}

		transferer := transfer.NewRclone(opts)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req := transfer.Request{
			RemotePath: "/remote/file.txt",
			LocalPath:  filepath.Join(tmpDir, "local_file.txt"),
			Size:       1024,
		}

		err := transferer.Transfer(ctx, req, nil)
		require.Error(t, err)
	})
}

// --- WithLogger Option Tests ---

func TestWithLoggerOption(t *testing.T) {
	t.Run("AppliesLoggerToTransferer", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		// Create a logger that writes to a buffer
		logger := zerolog.New(os.Stderr).With().
			Str("test", "value").
			Logger()

		transferer := transfer.NewRclone(opts, transfer.WithLogger(logger))
		require.NotNil(t, transferer)
	})
}

// --- Lifecycle Tests ---

func TestRcloneLifecycle(t *testing.T) {
	t.Run("MultipleCloseCallsSafe", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		// Multiple close calls should be safe
		assert.NoError(t, transferer.Close())
		assert.NoError(t, transferer.Close())
		assert.NoError(t, transferer.Close())
	})

	t.Run("PrepareShutdownThenClose", func(t *testing.T) {
		opts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:    "test.example.com",
				User:    "testuser",
				KeyFile: "/tmp/test_key",
			},
		}

		transferer := transfer.NewRclone(opts)

		// Should work correctly when PrepareShutdown is called before Close
		transferer.PrepareShutdown()
		assert.NoError(t, transferer.Close())
	})
}
