// Package cmd provides the CLI entry point.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/server"
	"github.com/seedreap/seedreap/ui"
)

const defaultShutdownTimeout = 30 * time.Second

// Version information - set at build time via ldflags.
//
//nolint:gochecknoglobals // build-time variables set via ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
	BuiltBy   = "unknown"
)

//nolint:gochecknoglobals // cobra CLI flags require package-level variables
var (
	cfgFile   string
	logLevel  string
	logPretty bool
	listen    string

	showVersion bool
	appConfig   config.Config
)

// rootCmd represents the base command.
//
//nolint:gochecknoglobals // cobra requires package-level command variable
var rootCmd = &cobra.Command{
	Use:   "seedreap",
	Short: "Reap what your seedbox has sown",
	Long: `SeedReap monitors download clients (like qBittorrent) for completed
downloads and syncs them to local storage using parallel LFTP transfers.
Once synced, it triggers applications (like Sonarr/Radarr) to import
the files.

Inspired by seedsync (https://github.com/ipsingh06/seedsync).`,
	SilenceUsage: true,
	RunE:         run,
}

// Execute runs the root command.
func Execute() {
	// Check for version flag early to avoid config loading
	for _, arg := range os.Args[1:] {
		if arg == "-V" || arg == "--version" {
			printVersion()
			return
		}
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

//nolint:gochecknoinits // cobra requires init for flag registration
func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.Flags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.seedreap.yaml)")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "V", false, "print version information and exit")
	rootCmd.Flags().StringVar(&listen, "listen", "", "address to listen on (default \"[::]:8423\")")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.Flags().BoolVar(&logPretty, "log-pretty", false, "enable pretty (human-readable) logging")
}

func run(_ *cobra.Command, _ []string) error {
	// Handle version flag
	if showVersion {
		printVersion()
		return nil
	}

	opts := server.Options{
		UIFS:   ui.FS,
		UIPath: "dist",
		Logger: log.With().Str("component", "main").Logger(),
	}

	srv, err := server.New(appConfig, opts)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Handle repeated signals during shutdown - force exit on second signal
	go func() {
		<-sigCh
		// Prepare for graceful shutdown (suppresses expected cancellation errors)
		srv.PrepareShutdown()
		cancel()

		// Wait for second signal
		<-sigCh
		log.Warn().Msg("received second signal, forcing exit")
		os.Exit(1)
	}()

	// Run server
	if err = srv.Run(ctx); err != nil {
		return err
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer shutdownCancel()

	return srv.Shutdown(shutdownCtx)
}

//nolint:forbidigo // CLI version output requires fmt.Printf
func printVersion() {
	fmt.Printf("seedreap %s\n", Version)
	fmt.Printf("  commit:   %s\n", Commit)
	fmt.Printf("  built:    %s\n", BuildDate)
	fmt.Printf("  built by: %s\n", BuiltBy)
}

func initConfig() {
	// Load config from file and environment variables
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: cfgFile,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Apply CLI flag overrides
	if listen != "" {
		cfg.Server.Listen = listen
	}

	appConfig = cfg

	// Setup logging based on CLI flags
	setupLogging()
}

func setupLogging() {
	// Set log level based on CLI flag
	switch strings.ToLower(logLevel) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Setup output based on CLI flag
	if logPretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr}) //nolint:reassign // standard zerolog pattern
	}
}
