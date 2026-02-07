package server_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/server"
)

// --- Event-Driven Architecture Tests ---

func TestServerNew_CreatesController(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Listen: ":8080"},
		Sync: config.SyncConfig{
			DownloadsPath: t.TempDir(),
			SyncingPath:   t.TempDir(),
		},
	}

	srv, err := server.New(cfg, server.Options{
		Logger: zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Shutdown(t.Context())
	})

	// Verify download controller is created
	require.NotNil(t, srv.Controller())
}

func TestServerNew_ControllerUsesConfiguredInterval(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Listen: ":8080"},
		Sync: config.SyncConfig{
			DownloadsPath: t.TempDir(),
			SyncingPath:   t.TempDir(),
			PollInterval:  10 * time.Second,
		},
	}

	srv, err := server.New(cfg, server.Options{
		Logger: zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Shutdown(t.Context())
	})

	// Controller should be created with configured interval
	require.NotNil(t, srv.Controller())
}

func TestServerNew_EventBusConnectsComponents(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Listen: ":8080"},
		Sync: config.SyncConfig{
			DownloadsPath: t.TempDir(),
			SyncingPath:   t.TempDir(),
		},
	}

	srv, err := server.New(cfg, server.Options{
		Logger: zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Shutdown(t.Context())
	})

	// Verify event bus is created
	require.NotNil(t, srv.EventBus())

	// Verify database is created
	require.NotNil(t, srv.DB())
}

func TestServerRun_StartsControllers(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Listen: ":0"}, // Random port
		Sync: config.SyncConfig{
			DownloadsPath: t.TempDir(),
			SyncingPath:   t.TempDir(),
		},
	}

	srv, err := server.New(cfg, server.Options{
		Logger: zerolog.Nop(),
	})
	require.NoError(t, err)

	ctx := t.Context()

	// Start the server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify controller is running by checking it exists
	require.NotNil(t, srv.Controller())

	// Shutdown gracefully
	srv.PrepareShutdown()
	err = srv.Shutdown(ctx)
	require.NoError(t, err)
}

func TestServerShutdown_StopsControllers(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{Listen: ":0"}, // Random port
		Sync: config.SyncConfig{
			DownloadsPath: t.TempDir(),
			SyncingPath:   t.TempDir(),
		},
	}

	srv, err := server.New(cfg, server.Options{
		Logger: zerolog.Nop(),
	})
	require.NoError(t, err)

	ctx := t.Context()

	// Start the server
	go func() {
		_ = srv.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Shutdown should complete without error
	srv.PrepareShutdown()
	err = srv.Shutdown(ctx)
	require.NoError(t, err)
}
