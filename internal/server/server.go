// Package server provides the main application server.
package server

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/ent"
	"github.com/seedreap/seedreap/internal/ent/generated"
	appschema "github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/events"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/tracker"
	"github.com/seedreap/seedreap/internal/transfer"
)

const defaultPollInterval = 30 * time.Second

// Options holds additional server options not in config.
type Options struct {
	// UI filesystem (optional)
	UIFS   embed.FS
	UIPath string

	// Logger
	Logger zerolog.Logger
}

// Server is the main application server.
type Server struct {
	cfg               config.Config
	opts              Options
	db                *generated.Client
	eventBus          *events.Bus
	httpServer        *HTTPServer
	controller        *download.Controller
	syncController    *filesync.Controller
	appController     *app.Controller
	eventsController  *events.Controller
	trackerController *tracker.Controller
	syncer            *filesync.Syncer
	logger            zerolog.Logger
}

// New creates a new server with the given configuration.
//
//nolint:funlen // initialization function needs to set up multiple components
func New(cfg config.Config, opts Options) (*Server, error) {
	logger := opts.Logger
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}

	// Create database client
	db, err := ent.OpenSQLite(cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Run migrations
	if err = ent.Migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}
	logger.Info().Str("dsn", cfg.Database.DSN).Msg("database initialized")

	// Create event bus
	eventBus := events.New(
		events.WithLogger(logger.With().Str("component", "events").Logger()),
	)

	// Create events controller first (persists all events to timeline via database)
	eventsController := events.NewController(
		eventBus,
		db,
		events.WithControllerLogger(logger.With().Str("component", "events-controller").Logger()),
	)

	// Load configuration into database
	if err = loadConfigIntoDatabase(context.Background(), db, cfg, logger); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to load config into database: %w", err)
	}

	// Get poll interval
	pollInterval := cfg.Sync.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}

	// Create download controller (polls downloaders and emits download events via database)
	controller := download.NewController(
		eventBus,
		db,
		download.WithControllerLogger(logger.With().Str("component", "controller").Logger()),
		download.WithPollInterval(pollInterval),
		download.WithBaseDownloadsPath(cfg.Sync.DownloadsPath),
	)

	// Get SSH config from controller for file transfers
	transferSSHConfig := controller.SSHConfig()

	// Log configuration summary
	logger.Info().
		Int("downloaders", len(cfg.Downloaders)).
		Int("apps", len(cfg.Apps)).
		Msg("configuration loaded")

	if len(cfg.Apps) == 0 {
		logger.Warn().Msg("no apps configured - downloads will be synced but not imported to any app")
	}

	// Create syncer
	maxConcurrent := cfg.Sync.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = 2
	}

	parallelConnections := cfg.Sync.ParallelConnections
	if parallelConnections == 0 {
		parallelConnections = 8
	}

	// Create the transfer backend
	var transferer transfer.Transferer
	if transferSSHConfig.Host != "" {
		transferOpts := transfer.Options{
			SSH: transfer.SSHConfig{
				Host:           transferSSHConfig.Host,
				Port:           transferSSHConfig.Port,
				User:           transferSSHConfig.User,
				KeyFile:        transferSSHConfig.KeyFile,
				KnownHostsFile: transferSSHConfig.KnownHostsFile,
				IgnoreHostKey:  transferSSHConfig.IgnoreHostKey,
			},
			ParallelConnections: parallelConnections,
			SpeedLimit:          cfg.Sync.TransferSpeedMax,
		}

		transferLogger := logger.With().Str("component", "transfer").Logger()

		// rclone is the default (and currently only) transfer backend
		transferer = transfer.NewRclone(transferOpts, transfer.WithLogger(transferLogger))
		logger.Info().
			Str("backend", "rclone").
			Str("host", transferSSHConfig.Host).
			Int("port", transferSSHConfig.Port).
			Str("user", transferSSHConfig.User).
			Int("parallel_connections", parallelConnections).
			Msg("transfer backend configured")
	}

	syncerOpts := []filesync.Option{
		filesync.WithLogger(logger.With().Str("component", "syncer").Logger()),
		filesync.WithMaxConcurrent(maxConcurrent),
	}

	if transferer != nil {
		syncerOpts = append(syncerOpts, filesync.WithTransferer(transferer))
	}

	syncr := filesync.New(cfg.Sync.SyncingPath, syncerOpts...)

	// Create filesync controller (manages sync jobs/files and transfers via database)
	syncControllerOpts := []filesync.ControllerOption{
		filesync.WithControllerLogger(logger.With().Str("component", "sync-controller").Logger()),
		filesync.WithControllerSyncingPath(cfg.Sync.SyncingPath),
		filesync.WithControllerDownloadsPath(cfg.Sync.DownloadsPath),
		filesync.WithControllerMaxConcurrent(maxConcurrent),
		filesync.WithControllerSyncer(syncr), // For live progress tracking
	}
	if transferer != nil {
		syncControllerOpts = append(syncControllerOpts, filesync.WithControllerTransferer(transferer))
	}
	syncController := filesync.NewController(eventBus, db, syncControllerOpts...)

	// Create app controller (handles app notifications via database)
	appController := app.NewController(
		eventBus,
		db,
		app.WithControllerLogger(logger.With().Str("component", "app-controller").Logger()),
	)

	// Create tracker controller (maintains TrackedDownload state via database)
	trackerController := tracker.NewController(
		eventBus,
		db,
		tracker.WithControllerLogger(logger.With().Str("component", "tracker-controller").Logger()),
	)

	// Create HTTP server
	httpOpts := []HTTPOption{
		WithHTTPLogger(logger.With().Str("component", "api").Logger()),
		WithHTTPDB(db),
	}

	if opts.UIFS != (embed.FS{}) {
		httpOpts = append(httpOpts, WithUI(opts.UIFS, opts.UIPath))
	}

	httpServer := NewHTTPServer(
		syncr,
		httpOpts...,
	)

	return &Server{
		cfg:               cfg,
		opts:              opts,
		db:                db,
		eventBus:          eventBus,
		httpServer:        httpServer,
		controller:        controller,
		syncController:    syncController,
		appController:     appController,
		eventsController:  eventsController,
		trackerController: trackerController,
		syncer:            syncr,
		logger:            logger,
	}, nil
}

// Run starts the server and blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info().
		Str("listen", s.cfg.Server.Listen).
		Str("downloads_path", s.cfg.Sync.DownloadsPath).
		Str("syncing_path", s.cfg.Sync.SyncingPath).
		Msg("starting seedreap")

	// Start events controller first (records all events to timeline)
	if err := s.eventsController.Start(ctx); err != nil {
		return fmt.Errorf("failed to start events controller: %w", err)
	}

	// Start tracker controller (maintains TrackedDownload state)
	if err := s.trackerController.Start(ctx); err != nil {
		_ = s.eventsController.Stop()
		return fmt.Errorf("failed to start tracker controller: %w", err)
	}

	// Start app controller (listens for MoveComplete events)
	if err := s.appController.Start(ctx); err != nil {
		_ = s.trackerController.Stop()
		_ = s.eventsController.Stop()
		return fmt.Errorf("failed to start app controller: %w", err)
	}

	// Start filesync controller (handles file sync, move, cleanup, and category changes)
	if err := s.syncController.Start(ctx); err != nil {
		_ = s.appController.Stop()
		_ = s.trackerController.Stop()
		_ = s.eventsController.Stop()
		return fmt.Errorf("failed to start filesync controller: %w", err)
	}

	// Start download controller last (emits download events)
	if err := s.controller.Start(ctx); err != nil {
		_ = s.syncController.Stop()
		_ = s.appController.Stop()
		_ = s.trackerController.Stop()
		_ = s.eventsController.Stop()
		return fmt.Errorf("failed to start download controller: %w", err)
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Start(s.cfg.Server.Listen); err != nil {
			errCh <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.logger.Info().Msg("received shutdown signal")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// PrepareShutdown prepares for graceful shutdown by suppressing expected errors.
// Call this before cancelling the main context.
func (s *Server) PrepareShutdown() {
	s.syncer.PrepareShutdown()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down...")

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error().Err(err).Msg("server shutdown error")
	}

	// Stop in reverse order of startup (publishers first, then subscribers)

	// Stop download controller first to stop emitting new events
	if err := s.controller.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("download controller stop error")
	}

	// Stop filesync controller (can still process pending sync events)
	if err := s.syncController.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("filesync controller stop error")
	}

	// Stop app controller (can still process pending MoveComplete events)
	if err := s.appController.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("app controller stop error")
	}

	// Stop tracker controller
	if err := s.trackerController.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("tracker controller stop error")
	}

	// Stop events controller last (records events until the very end)
	if err := s.eventsController.Stop(); err != nil {
		s.logger.Error().Err(err).Msg("events controller stop error")
	}

	// Close the syncer to release transfer backend resources
	if err := s.syncer.Close(); err != nil {
		s.logger.Error().Err(err).Msg("syncer close error")
	}

	// Close the event bus
	s.eventBus.Close()

	// Close the database client
	if err := s.db.Close(); err != nil {
		s.logger.Error().Err(err).Msg("database close error")
	}

	s.logger.Info().Msg("shutdown complete")
	return nil
}

// DB returns the Ent database client.
func (s *Server) DB() *generated.Client {
	return s.db
}

// EventBus returns the event bus.
func (s *Server) EventBus() *events.Bus {
	return s.eventBus
}

// Controller returns the download controller.
func (s *Server) Controller() *download.Controller {
	return s.controller
}

// loadConfigIntoDatabase loads downloaders and apps from config into the database.
func loadConfigIntoDatabase(ctx context.Context, db *generated.Client, cfg config.Config, logger zerolog.Logger) error {
	// Load downloaders
	for name, dlCfg := range cfg.Downloaders {
		_, err := db.DownloadClient.Create().
			SetName(name).
			SetType(dlCfg.Type).
			SetURL(dlCfg.URL).
			SetUsername(dlCfg.Username).
			SetPassword(dlCfg.Password).
			SetHTTPTimeout(int64(dlCfg.HTTPTimeout.Seconds())).
			SetEnabled(true).
			SetSSHHost(dlCfg.SSH.Host).
			SetSSHPort(dlCfg.SSH.Port).
			SetSSHUser(dlCfg.SSH.User).
			SetSSHKeyFile(dlCfg.SSH.KeyFile).
			SetSSHKnownHostsFile(dlCfg.SSH.KnownHostsFile).
			SetSSHIgnoreHostKey(dlCfg.SSH.IgnoreHostKey).
			SetSSHTimeout(int64(dlCfg.SSH.Timeout.Seconds())).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to create downloader %q: %w", name, err)
		}

		logger.Debug().
			Str("name", name).
			Str("type", dlCfg.Type).
			Msg("loaded downloader into database")
	}

	// Load apps
	for name, appCfg := range cfg.Apps {
		_, err := db.App.Create().
			SetName(name).
			SetType(appschema.Type(appCfg.Type)).
			SetURL(appCfg.URL).
			SetAPIKey(appCfg.APIKey).
			SetCategory(appCfg.Category).
			SetDownloadsPath(appCfg.DownloadsPath).
			SetHTTPTimeout(int64(appCfg.HTTPTimeout.Seconds())).
			SetCleanupOnCategoryChange(appCfg.CleanupOnCategoryChange).
			SetCleanupOnRemove(appCfg.CleanupOnRemove).
			SetEnabled(true).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to create app %q: %w", name, err)
		}

		logger.Debug().
			Str("name", name).
			Str("type", appCfg.Type).
			Str("category", appCfg.Category).
			Msg("loaded app into database")
	}

	logger.Info().
		Int("downloaders", len(cfg.Downloaders)).
		Int("apps", len(cfg.Apps)).
		Msg("configuration loaded into database")

	return nil
}
