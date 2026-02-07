// Package config provides application configuration.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Default configuration values.
const (
	DefaultHTTPTimeout   = 30 * time.Second
	DefaultSSHTimeout    = 10 * time.Second
	DefaultSSHPort       = 22
	DefaultMaxConcurrent = 2
)

// Config is the application configuration.
type Config struct {
	Server      ServerConfig                `mapstructure:"server"`
	Database    DatabaseConfig              `mapstructure:"database"`
	Downloaders map[string]DownloaderConfig `mapstructure:"downloaders"`
	Apps        map[string]AppEntryConfig   `mapstructure:"apps"`
	Sync        SyncConfig                  `mapstructure:"sync"`
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	// DSN is the database connection string.
	// For SQLite: "file:path/to/db.sqlite" or "file::memory:?cache=shared" for in-memory.
	// Default: "file::memory:?cache=shared" (shared in-memory database)
	DSN string `mapstructure:"dsn"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Listen string `mapstructure:"listen"`
}

// SyncConfig holds sync-related configuration.
type SyncConfig struct {
	DownloadsPath       string        `mapstructure:"downloadsPath"`
	SyncingPath         string        `mapstructure:"syncingPath"`
	MaxConcurrent       int           `mapstructure:"maxConcurrent"`
	PollInterval        time.Duration `mapstructure:"pollInterval"`
	TransferSpeedMax    int64         `mapstructure:"transferSpeedMax"`    // bytes/sec per file, 0 = unlimited (total max = this * maxConcurrent)
	ParallelConnections int           `mapstructure:"parallelConnections"` // parallel connections per file (default 8)
	TransferBackend     string        `mapstructure:"transferBackend"`     // transfer backend: "rclone" (default)
}

// DownloaderConfig holds configuration for a downloader instance.
type DownloaderConfig struct {
	Type        string        `mapstructure:"type"`
	URL         string        `mapstructure:"url"`
	Username    string        `mapstructure:"username"`
	Password    string        `mapstructure:"password"`
	HTTPTimeout time.Duration `mapstructure:"httpTimeout"`
	SSH         SSHConfig     `mapstructure:"ssh"`
}

// SSHConfig holds SSH connection configuration.
type SSHConfig struct {
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	User           string        `mapstructure:"user"`
	KeyFile        string        `mapstructure:"keyFile"`
	KnownHostsFile string        `mapstructure:"knownHostsFile"` // Path to known_hosts file (mutually exclusive with IgnoreHostKey)
	IgnoreHostKey  bool          `mapstructure:"ignoreHostKey"`  // Skip host key verification (mutually exclusive with KnownHostsFile)
	Timeout        time.Duration `mapstructure:"timeout"`
}

// AppEntryConfig holds configuration for an application instance.
type AppEntryConfig struct {
	Type                    string        `mapstructure:"type"`
	URL                     string        `mapstructure:"url"`
	APIKey                  string        `mapstructure:"apiKey"`
	Category                string        `mapstructure:"category"`
	DownloadsPath           string        `mapstructure:"downloadsPath"`           // Override path, defaults to global downloadsPath/<category>
	HTTPTimeout             time.Duration `mapstructure:"httpTimeout"`             // HTTP client timeout
	CleanupOnCategoryChange bool          `mapstructure:"cleanupOnCategoryChange"` // Delete synced files when category changes (default: false)
	CleanupOnRemove         bool          `mapstructure:"cleanupOnRemove"`         // Delete synced files when removed from downloader (default: false)
}

// LoadOptions configures how configuration is loaded.
type LoadOptions struct {
	// ConfigFile is an explicit config file path. If empty, default locations are searched.
	ConfigFile string
}

// Load reads configuration from file and environment variables.
// If opts.ConfigFile is set, that file is used directly.
// Otherwise, it searches default locations: $HOME, current directory, /config
// for files named .seedreap.yaml, seedreap.yaml, or config.yaml.
//
// Environment variables with prefix SEEDREAP_ override config file values.
// For dynamic maps (downloaders, apps), set SEEDREAP_DOWNLOADERS and SEEDREAP_APPS
// to comma-separated lists of names to enable env var binding for those entries.
func Load(opts LoadOptions) (Config, error) {
	v := viper.NewWithOptions(viper.ExperimentalBindStruct())

	if opts.ConfigFile != "" {
		v.SetConfigFile(opts.ConfigFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(home)
		}
		v.AddConfigPath(".")
		v.AddConfigPath("/config")
		v.SetConfigType("yaml")
		v.SetConfigName(".seedreap")
		v.SetConfigName("seedreap")
		v.SetConfigName("config")
	}

	// Environment variables
	v.SetEnvPrefix("SEEDREAP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind env vars for dynamic map keys if specified
	bindDownloaderEnvVars(v)
	bindAppEnvVars(v)

	// Set defaults
	v.SetDefault("server.listen", "[::]:8423")
	v.SetDefault("database.dsn", "file::memory:?cache=shared")
	v.SetDefault("sync.downloadsPath", "/downloads")
	v.SetDefault("sync.syncingPath", "/downloads/syncing")
	v.SetDefault("sync.maxConcurrent", DefaultMaxConcurrent)
	v.SetDefault("sync.pollInterval", "30s")

	// Read config file (ignore error if not found)
	_ = v.ReadInConfig()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}

	setDefaultsOnListConfigs(&cfg)

	if err := validate(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// setDefaultsOnListConfigs applies default values to config fields that can't
// be set with viper.setDefault.
func setDefaultsOnListConfigs(cfg *Config) {
	// Set defaults for downloaders
	for name, dl := range cfg.Downloaders {
		if dl.HTTPTimeout == 0 {
			dl.HTTPTimeout = DefaultHTTPTimeout
		}
		if dl.SSH.Port == 0 {
			dl.SSH.Port = DefaultSSHPort
		}
		if dl.SSH.Timeout == 0 {
			dl.SSH.Timeout = DefaultSSHTimeout
		}
		cfg.Downloaders[name] = dl
	}

	// Set defaults for apps
	for name, app := range cfg.Apps {
		if app.HTTPTimeout == 0 {
			app.HTTPTimeout = DefaultHTTPTimeout
		}
		cfg.Apps[name] = app
	}
}

// Valid downloader types.
//
//nolint:gochecknoglobals // validation lookup table
var validDownloaderTypes = map[string]bool{
	"qbittorrent": true,
}

// Valid app types.
//
//nolint:gochecknoglobals // validation lookup table
var validAppTypes = map[string]bool{
	"sonarr":      true,
	"radarr":      true,
	"passthrough": true,
}

// Valid transfer backends.
//
//nolint:gochecknoglobals // validation lookup table
var validTransferBackends = map[string]bool{
	"":       true, // empty means default (rclone)
	"rclone": true,
}

// validateURLScheme checks that a URL uses http or https scheme.
func validateURLScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must use http or https scheme, got %q", u.Scheme)
	}
	return nil
}

// rcloneSpecialChars are characters that could be used for injection in rclone connection strings.
const rcloneSpecialChars = ",=:;'\""

// validateSSHParam checks that an SSH parameter doesn't contain rclone special characters.
func validateSSHParam(name, value string) error {
	if strings.ContainsAny(value, rcloneSpecialChars) {
		return fmt.Errorf("%s contains invalid characters (cannot contain %q)", name, rcloneSpecialChars)
	}
	return nil
}

// validateSSHKeyFile checks that an SSH key file exists and has secure permissions.
func validateSSHKeyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("ssh key file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access ssh key file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("ssh key file is a directory: %s", path)
	}

	// Check permissions - should be 0600 or 0400 (owner read/write or read-only)
	mode := info.Mode().Perm()
	// Allow 0600, 0400, and slight variations that still restrict group/other access
	const maxAllowedPerm = fs.FileMode(0600)
	if mode&0077 != 0 {
		return fmt.Errorf("ssh key file has insecure permissions %04o (should be 0600 or 0400): %s", mode, path)
	}
	if mode > maxAllowedPerm {
		return fmt.Errorf("ssh key file has insecure permissions %04o (should be 0600 or 0400): %s", mode, path)
	}

	return nil
}

// validateKnownHostsFile checks that a known_hosts file exists.
func validateKnownHostsFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("known_hosts file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access known_hosts file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("known_hosts file is a directory: %s", path)
	}

	return nil
}

// validate checks that the configuration is valid.
//
//nolint:gocognit,gocyclo,cyclop // validation requires checking many fields
func validate(cfg *Config) error {
	var errs []error

	// Validate downloaders
	for name, dl := range cfg.Downloaders {
		if dl.Type == "" {
			errs = append(errs, fmt.Errorf("downloader %q: type is required", name))
		} else if !validDownloaderTypes[dl.Type] {
			errs = append(errs, fmt.Errorf("downloader %q: unknown type %q", name, dl.Type))
		}

		if dl.URL == "" {
			errs = append(errs, fmt.Errorf("downloader %q: url is required", name))
		} else if err := validateURLScheme(dl.URL); err != nil {
			errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
		}

		// SSH config is required for file transfers
		if dl.SSH.Host == "" {
			errs = append(errs, fmt.Errorf("downloader %q: ssh.host is required", name))
		} else if err := validateSSHParam("ssh.host", dl.SSH.Host); err != nil {
			errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
		}

		if dl.SSH.User == "" {
			errs = append(errs, fmt.Errorf("downloader %q: ssh.user is required", name))
		} else if err := validateSSHParam("ssh.user", dl.SSH.User); err != nil {
			errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
		}

		if dl.SSH.KeyFile == "" {
			errs = append(errs, fmt.Errorf("downloader %q: ssh.keyFile is required", name))
		} else {
			if err := validateSSHParam("ssh.keyFile", dl.SSH.KeyFile); err != nil {
				errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
			}
			if err := validateSSHKeyFile(dl.SSH.KeyFile); err != nil {
				errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
			}
		}

		// Host key verification: must specify knownHostsFile OR ignoreHostKey, but not both
		if dl.SSH.KnownHostsFile != "" && dl.SSH.IgnoreHostKey {
			errs = append(errs, fmt.Errorf(
				"downloader %q: ssh.knownHostsFile and ssh.ignoreHostKey are mutually exclusive", name))
		}
		if dl.SSH.KnownHostsFile == "" && !dl.SSH.IgnoreHostKey {
			errs = append(errs, fmt.Errorf(
				"downloader %q: ssh.knownHostsFile is required (or set ssh.ignoreHostKey to true)", name))
		}
		if dl.SSH.KnownHostsFile != "" {
			if err := validateSSHParam("ssh.knownHostsFile", dl.SSH.KnownHostsFile); err != nil {
				errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
			}
			if err := validateKnownHostsFile(dl.SSH.KnownHostsFile); err != nil {
				errs = append(errs, fmt.Errorf("downloader %q: %w", name, err))
			}
		}
	}

	// Validate apps
	for name, app := range cfg.Apps {
		if app.Type == "" {
			errs = append(errs, fmt.Errorf("app %q: type is required", name))
		} else if !validAppTypes[app.Type] {
			errs = append(errs, fmt.Errorf("app %q: unknown type %q", name, app.Type))
		}

		if app.Category == "" {
			errs = append(errs, fmt.Errorf("app %q: category is required", name))
		}

		// URL and API key required for non-passthrough apps
		if app.Type != "passthrough" {
			if app.URL == "" {
				errs = append(errs, fmt.Errorf("app %q: url is required", name))
			} else if err := validateURLScheme(app.URL); err != nil {
				errs = append(errs, fmt.Errorf("app %q: %w", name, err))
			}

			if app.APIKey == "" {
				errs = append(errs, fmt.Errorf("app %q: apiKey is required", name))
			}
		}
	}

	// Validate sync config
	if cfg.Sync.DownloadsPath == "" {
		errs = append(errs, errors.New("sync.downloadsPath is required"))
	}
	if cfg.Sync.SyncingPath == "" {
		errs = append(errs, errors.New("sync.syncingPath is required"))
	}
	if !validTransferBackends[cfg.Sync.TransferBackend] {
		errs = append(errs, fmt.Errorf("sync.transferBackend: unknown backend %q", cfg.Sync.TransferBackend))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// downloaderEnvFields lists all DownloaderConfig fields for env var binding.
// This must be kept in sync with DownloaderConfig and SSHConfig structs.
// Tests verify this list matches the struct fields.
//
//nolint:gochecknoglobals // env var binding field list
var downloaderEnvFields = []string{
	"type",
	"url",
	"username",
	"password",
	"httpTimeout",
	"ssh.host",
	"ssh.port",
	"ssh.user",
	"ssh.keyFile",
	"ssh.knownHostsFile",
	"ssh.ignoreHostKey",
	"ssh.timeout",
}

// appEnvFields lists all AppEntryConfig fields for env var binding.
// This must be kept in sync with AppEntryConfig struct.
// Tests verify this list matches the struct fields.
//
//nolint:gochecknoglobals // env var binding field list
var appEnvFields = []string{
	"type",
	"url",
	"apiKey",
	"category",
	"downloadsPath",
	"httpTimeout",
	"cleanupOnCategoryChange",
	"cleanupOnRemove",
}

// bindDownloaderEnvVars reads SEEDREAP_DOWNLOADERS env var to get the list of
// downloader names, then binds all downloader fields for each name using MustBindEnv.
// This allows viper to discover dynamic map keys from environment variables.
// The list env var is unset after reading to prevent viper from treating it as
// the "downloaders" config key (which would cause a type mismatch).
func bindDownloaderEnvVars(v *viper.Viper) {
	downloadersEnv := os.Getenv("SEEDREAP_DOWNLOADERS")
	if downloadersEnv == "" {
		return
	}

	// Unset the list env var so viper doesn't interpret it as downloaders=string
	_ = os.Unsetenv("SEEDREAP_DOWNLOADERS")

	for name := range strings.SplitSeq(downloadersEnv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		for _, field := range downloaderEnvFields {
			key := "downloaders." + name + "." + field
			v.MustBindEnv(key)
		}
	}
}

// bindAppEnvVars reads SEEDREAP_APPS env var to get the list of app names,
// then binds all app fields for each name using MustBindEnv.
// This allows viper to discover dynamic map keys from environment variables.
// The list env var is unset after reading to prevent viper from treating it as
// the "apps" config key (which would cause a type mismatch).
func bindAppEnvVars(v *viper.Viper) {
	appsEnv := os.Getenv("SEEDREAP_APPS")
	if appsEnv == "" {
		return
	}

	// Unset the list env var so viper doesn't interpret it as apps=string
	_ = os.Unsetenv("SEEDREAP_APPS")

	for name := range strings.SplitSeq(appsEnv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		for _, field := range appEnvFields {
			key := "apps." + name + "." + field
			v.MustBindEnv(key)
		}
	}
}
