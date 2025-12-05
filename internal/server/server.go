// Package server provides the main application server.
package server

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/api"
	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/orchestrator"
	"github.com/seedreap/seedreap/internal/timeline"
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
	cfg          config.Config
	opts         Options
	apiServer    *api.Server
	orchestrator *orchestrator.Orchestrator
	syncer       *filesync.Syncer
	logger       zerolog.Logger
}

// New creates a new server with the given configuration.
//
//nolint:funlen // initialization function needs to set up multiple components
func New(cfg config.Config, opts Options) (*Server, error) {
	logger := opts.Logger
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}

	// Create registries
	dlRegistry := download.NewRegistry()
	appRegistry := app.NewRegistry()

	// Track SSH config for file transfers (use first downloader's config)
	var transferSSHConfig config.SSHConfig

	// Build downloaders from config
	for name, dlCfg := range cfg.Downloaders {
		logger.Debug().Str("name", name).Str("type", dlCfg.Type).Msg("configuring downloader")

		switch dlCfg.Type {
		case "qbittorrent":
			// Store SSH config for file transfers
			if transferSSHConfig.Host == "" {
				transferSSHConfig = dlCfg.SSH
			}

			client := download.NewQBittorrent(
				name,
				dlCfg,
				download.WithLogger(logger.With().Str("downloader", name).Logger()),
			)
			dlRegistry.Register(name, client)

		default:
			logger.Warn().Str("type", dlCfg.Type).Msg("unknown downloader type")
		}
	}

	// Build apps from config
	for name, appCfg := range cfg.Apps {
		logger.Info().
			Str("name", name).
			Str("type", appCfg.Type).
			Str("category", appCfg.Category).
			Msg("configuring app")

		// Build app config from entry config
		arrCfg := app.ArrConfig{
			URL:           appCfg.URL,
			APIKey:        appCfg.APIKey,
			Category:      appCfg.Category,
			DownloadsPath: appCfg.DownloadsPath,
			HTTPTimeout:   appCfg.HTTPTimeout,
		}

		switch appCfg.Type {
		case "sonarr":
			client := app.NewSonarr(
				name,
				arrCfg,
				app.WithLogger(logger.With().Str("app", name).Logger()),
				app.WithCleanupOnCategoryChange(appCfg.CleanupOnCategoryChange),
				app.WithCleanupOnRemove(appCfg.CleanupOnRemove),
			)
			appRegistry.Register(name, client)

		case "radarr":
			client := app.NewRadarr(
				name,
				arrCfg,
				app.WithLogger(logger.With().Str("app", name).Logger()),
				app.WithCleanupOnCategoryChange(appCfg.CleanupOnCategoryChange),
				app.WithCleanupOnRemove(appCfg.CleanupOnRemove),
			)
			appRegistry.Register(name, client)

		case "passthrough":
			client := app.NewPassthrough(
				name,
				appCfg.Category,
				appCfg.DownloadsPath,
				app.WithLogger(logger.With().Str("app", name).Logger()),
				app.WithCleanupOnCategoryChange(appCfg.CleanupOnCategoryChange),
				app.WithCleanupOnRemove(appCfg.CleanupOnRemove),
			)
			appRegistry.Register(name, client)

		default:
			logger.Warn().Str("type", appCfg.Type).Msg("unknown app type")
		}
	}

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

	// Create timeline recorder
	timelineRecorder := timeline.NewRecorder(
		timeline.WithLogger(logger.With().Str("component", "timeline").Logger()),
	)

	// Create orchestrator
	pollInterval := cfg.Sync.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}

	orch := orchestrator.New(
		dlRegistry,
		appRegistry,
		syncr,
		cfg.Sync.DownloadsPath,
		orchestrator.WithLogger(logger.With().Str("component", "orchestrator").Logger()),
		orchestrator.WithPollInterval(pollInterval),
		orchestrator.WithTimeline(timelineRecorder),
	)

	// Create API server
	apiOpts := []api.Option{
		api.WithLogger(logger.With().Str("component", "api").Logger()),
	}

	if opts.UIFS != (embed.FS{}) {
		apiOpts = append(apiOpts, api.WithUI(opts.UIFS, opts.UIPath))
	}

	apiServer := api.New(
		orch,
		dlRegistry,
		appRegistry,
		syncr,
		apiOpts...,
	)

	return &Server{
		cfg:          cfg,
		opts:         opts,
		apiServer:    apiServer,
		orchestrator: orch,
		syncer:       syncr,
		logger:       logger,
	}, nil
}

// Run starts the server and blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info().
		Str("listen", s.cfg.Server.Listen).
		Str("downloads_path", s.cfg.Sync.DownloadsPath).
		Str("syncing_path", s.cfg.Sync.SyncingPath).
		Msg("starting seedreap")

	// Start orchestrator
	if err := s.orchestrator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start orchestrator: %w", err)
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.apiServer.Start(s.cfg.Server.Listen); err != nil {
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

	if err := s.apiServer.Shutdown(ctx); err != nil {
		s.logger.Error().Err(err).Msg("server shutdown error")
	}

	s.orchestrator.Stop()

	// Close the syncer to release transfer backend resources
	if err := s.syncer.Close(); err != nil {
		s.logger.Error().Err(err).Msg("syncer close error")
	}

	s.logger.Info().Msg("shutdown complete")
	return nil
}
