package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
)

// loadConfigFromYAML creates a temp config file and loads it using Load().
// This ensures tests use the exact same config loading code as the application.
func loadConfigFromYAML(t *testing.T, yaml string) config.Config {
	t.Helper()

	// Create temp directory for config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write YAML to temp file
	err := os.WriteFile(configFile, []byte(yaml), 0644)
	require.NoError(t, err, "failed to write temp config file")

	// Load using the same function the app uses
	cfg, err := config.Load(config.LoadOptions{ConfigFile: configFile})
	require.NoError(t, err, "failed to load config")

	return cfg
}

func TestConfigDefaults(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg config.Config)
	}{
		{
			name: "empty config uses all defaults",
			yaml: "",
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "[::]:8423", cfg.Server.Listen)
				assert.Equal(t, "/downloads", cfg.Sync.DownloadsPath)
				assert.Equal(t, "/downloads/syncing", cfg.Sync.SyncingPath)
				assert.Equal(t, 2, cfg.Sync.MaxConcurrent)
				assert.Equal(t, 30*time.Second, cfg.Sync.PollInterval)
			},
		},
		{
			name: "server listen can be overridden",
			yaml: `
server:
  listen: "0.0.0.0:9000"
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "0.0.0.0:9000", cfg.Server.Listen)
				// Other defaults still apply
				assert.Equal(t, "/downloads", cfg.Sync.DownloadsPath)
			},
		},
		{
			name: "sync paths can be overridden",
			yaml: `
sync:
  downloadsPath: /data/downloads
  syncingPath: /data/.syncing
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "/data/downloads", cfg.Sync.DownloadsPath)
				assert.Equal(t, "/data/.syncing", cfg.Sync.SyncingPath)
				// Other defaults still apply
				assert.Equal(t, 2, cfg.Sync.MaxConcurrent)
			},
		},
		{
			name: "maxConcurrent can be overridden",
			yaml: `
sync:
  maxConcurrent: 4
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, 4, cfg.Sync.MaxConcurrent)
			},
		},
		{
			name: "pollInterval can be overridden",
			yaml: `
sync:
  pollInterval: 60s
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, 60*time.Second, cfg.Sync.PollInterval)
			},
		},
		{
			name: "parallelConnections defaults to zero (server applies default of 8)",
			yaml: "",
			check: func(t *testing.T, cfg config.Config) {
				// ParallelConnections has no viper default, defaults to Go zero value
				// Server.New() applies the actual default of 8
				assert.Equal(t, 0, cfg.Sync.ParallelConnections)
			},
		},
		{
			name: "parallelConnections can be set",
			yaml: `
sync:
  parallelConnections: 16
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, 16, cfg.Sync.ParallelConnections)
			},
		},
		{
			name: "transferSpeedMax defaults to zero (unlimited)",
			yaml: "",
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, int64(0), cfg.Sync.TransferSpeedMax)
			},
		},
		{
			name: "transferSpeedMax can be set",
			yaml: `
sync:
  transferSpeedMax: 10485760
`,
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, int64(10485760), cfg.Sync.TransferSpeedMax)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAML(t, tt.yaml)
			tt.check(t, cfg)
		})
	}
}

func TestDownloaderConfig(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg config.Config)
	}{
		{
			name: "single qbittorrent downloader",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    username: admin
    password: secret
    ssh:
      host: seedbox.example.com
      port: 22
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Downloaders, 1)
				require.Contains(t, cfg.Downloaders, "seedbox")

				dl := cfg.Downloaders["seedbox"]
				assert.Equal(t, "qbittorrent", dl.Type)
				assert.Equal(t, "http://seedbox:8080", dl.URL)
				assert.Equal(t, "admin", dl.Username)
				assert.Equal(t, "secret", dl.Password)
				assert.Equal(t, "seedbox.example.com", dl.SSH.Host)
				assert.Equal(t, 22, dl.SSH.Port)
				assert.Equal(t, "seeduser", dl.SSH.User)
				assert.Equal(t, "/path/to/key", dl.SSH.KeyFile)
			},
		},
		{
			name: "multiple downloaders",
			yaml: `
downloaders:
  seedbox1:
    type: qbittorrent
    url: http://seedbox1:8080
    ssh:
      host: seedbox1.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
  seedbox2:
    type: qbittorrent
    url: http://seedbox2:8080
    ssh:
      host: seedbox2.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Downloaders, 2)
				assert.Contains(t, cfg.Downloaders, "seedbox1")
				assert.Contains(t, cfg.Downloaders, "seedbox2")
				assert.Equal(t, "http://seedbox1:8080", cfg.Downloaders["seedbox1"].URL)
				assert.Equal(t, "http://seedbox2:8080", cfg.Downloaders["seedbox2"].URL)
			},
		},
		{
			name: "ssh port defaults to 22 when not specified",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			check: func(t *testing.T, cfg config.Config) {
				dl := cfg.Downloaders["seedbox"]
				// Port defaults to 22 when not specified
				assert.Equal(t, config.DefaultSSHPort, dl.SSH.Port)
			},
		},
		{
			name: "optional credentials can be omitted",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			check: func(t *testing.T, cfg config.Config) {
				dl := cfg.Downloaders["seedbox"]
				assert.Empty(t, dl.Username)
				assert.Empty(t, dl.Password)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAML(t, tt.yaml)
			tt.check(t, cfg)
		})
	}
}

func TestAppConfig(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg config.Config)
	}{
		{
			name: "sonarr app",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Apps, 1)
				require.Contains(t, cfg.Apps, "sonarr")

				app := cfg.Apps["sonarr"]
				assert.Equal(t, "sonarr", app.Type)
				assert.Equal(t, "http://sonarr:8989", app.URL)
				assert.Equal(t, "abc123", app.APIKey)
				assert.Equal(t, "tv-sonarr", app.Category)
			},
		},
		{
			name: "radarr app",
			yaml: `
apps:
  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: xyz789
    category: movies-radarr
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Apps, 1)
				app := cfg.Apps["radarr"]
				assert.Equal(t, "radarr", app.Type)
				assert.Equal(t, "http://radarr:7878", app.URL)
				assert.Equal(t, "xyz789", app.APIKey)
				assert.Equal(t, "movies-radarr", app.Category)
			},
		},
		{
			name: "passthrough app",
			yaml: `
apps:
  misc:
    type: passthrough
    category: misc
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Apps, 1)
				app := cfg.Apps["misc"]
				assert.Equal(t, "passthrough", app.Type)
				assert.Equal(t, "misc", app.Category)
				assert.Empty(t, app.URL)
				assert.Empty(t, app.APIKey)
			},
		},
		{
			name: "downloadsPath is empty by default (orchestrator computes path with downloader name)",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				// downloadsPath should be empty so orchestrator can compute:
				// {sync.downloadsPath}/{downloader_name}/{category}
				assert.Empty(
					t, app.DownloadsPath,
					"downloadsPath should be empty by default so orchestrator can include downloader name",
				)
			},
		},
		{
			name: "downloadsPath can be explicitly set",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
    downloadsPath: /custom/path/tv
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				assert.Equal(t, "/custom/path/tv", app.DownloadsPath)
			},
		},
		{
			name: "cleanupOnCategoryChange defaults to false",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				assert.False(t, app.CleanupOnCategoryChange)
			},
		},
		{
			name: "cleanupOnCategoryChange can be enabled",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
    cleanupOnCategoryChange: true
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				assert.True(t, app.CleanupOnCategoryChange)
			},
		},
		{
			name: "cleanupOnRemove defaults to false",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				assert.False(t, app.CleanupOnRemove)
			},
		},
		{
			name: "cleanupOnRemove can be enabled",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
    cleanupOnRemove: true
`,
			check: func(t *testing.T, cfg config.Config) {
				app := cfg.Apps["sonarr"]
				assert.True(t, app.CleanupOnRemove)
			},
		},
		{
			name: "multiple apps with different types",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: abc123
    category: tv-sonarr
    cleanupOnCategoryChange: true
  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: xyz789
    category: movies-radarr
    cleanupOnCategoryChange: true
  misc:
    type: passthrough
    category: misc
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Apps, 3)
				assert.Equal(t, "sonarr", cfg.Apps["sonarr"].Type)
				assert.Equal(t, "radarr", cfg.Apps["radarr"].Type)
				assert.Equal(t, "passthrough", cfg.Apps["misc"].Type)
			},
		},
		{
			name: "multiple apps can share same category",
			yaml: `
apps:
  sonarr-hd:
    type: sonarr
    url: http://sonarr-hd:8989
    apiKey: abc123
    category: tv
  sonarr-4k:
    type: sonarr
    url: http://sonarr-4k:8989
    apiKey: def456
    category: tv
`,
			check: func(t *testing.T, cfg config.Config) {
				require.Len(t, cfg.Apps, 2)
				assert.Equal(t, "tv", cfg.Apps["sonarr-hd"].Category)
				assert.Equal(t, "tv", cfg.Apps["sonarr-4k"].Category)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAML(t, tt.yaml)
			tt.check(t, cfg)
		})
	}
}

func TestFullConfig(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) config.LoadOptions
	}{
		{
			name: "from yaml file",
			setup: func(t *testing.T) config.LoadOptions {
				yaml := `
server:
  listen: "0.0.0.0:8080"

sync:
  downloadsPath: /data/downloads
  syncingPath: /data/.seedreap-syncing
  maxConcurrent: 4
  parallelConnections: 16
  pollInterval: 60s
  transferSpeedMax: 52428800

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox.example.com:8080
    username: admin
    password: secret123
    ssh:
      host: seedbox.example.com
      port: 2222
      user: seeduser
      keyFile: /config/ssh/id_ed25519
      ignoreHostKey: true

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: sonarr-api-key
    category: tv-sonarr
    cleanupOnCategoryChange: true
  radarr-4k:
    type: radarr
    url: http://radarr-4k:7878
    apiKey: radarr-4k-api-key
    category: movies-4k
    downloadsPath: /data/downloads/movies-4k
    cleanupOnCategoryChange: true
    cleanupOnRemove: true
  misc:
    type: passthrough
    category: misc
`
				// Create temp directory for config file
				tmpDir := t.TempDir()
				configFile := filepath.Join(tmpDir, "config.yaml")

				err := os.WriteFile(configFile, []byte(yaml), 0644)
				require.NoError(t, err)

				return config.LoadOptions{ConfigFile: configFile}
			},
		},
		{
			name: "from environment variables",
			setup: func(t *testing.T) config.LoadOptions {
				// Set all config values via environment variables
				// Single underscore for hierarchy (camelCase keys have no underscores)
				envVars := map[string]string{
					"SEEDREAP_SERVER_LISTEN":                          "0.0.0.0:8080",
					"SEEDREAP_SYNC_DOWNLOADSPATH":                     "/data/downloads",
					"SEEDREAP_SYNC_SYNCINGPATH":                       "/data/.seedreap-syncing",
					"SEEDREAP_SYNC_MAXCONCURRENT":                     "4",
					"SEEDREAP_SYNC_PARALLELCONNECTIONS":               "16",
					"SEEDREAP_SYNC_POLLINTERVAL":                      "60s",
					"SEEDREAP_SYNC_TRANSFERSPEEDMAX":                  "52428800",
					"SEEDREAP_DOWNLOADERS":                            "seedbox",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_TYPE":               "qbittorrent",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_URL":                "http://seedbox.example.com:8080",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_USERNAME":           "admin",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_PASSWORD":           "secret123",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_HOST":           "seedbox.example.com",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_PORT":           "2222",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_USER":           "seeduser",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_KEYFILE":        "/config/ssh/id_ed25519",
					"SEEDREAP_DOWNLOADERS_SEEDBOX_SSH_IGNOREHOSTKEY":  "true",
					"SEEDREAP_APPS":                                   "sonarr,radarr-4k,misc",
					"SEEDREAP_APPS_SONARR_TYPE":                       "sonarr",
					"SEEDREAP_APPS_SONARR_URL":                        "http://sonarr:8989",
					"SEEDREAP_APPS_SONARR_APIKEY":                     "sonarr-api-key",
					"SEEDREAP_APPS_SONARR_CATEGORY":                   "tv-sonarr",
					"SEEDREAP_APPS_SONARR_CLEANUPONCATEGORYCHANGE":    "true",
					"SEEDREAP_APPS_RADARR-4K_TYPE":                    "radarr",
					"SEEDREAP_APPS_RADARR-4K_URL":                     "http://radarr-4k:7878",
					"SEEDREAP_APPS_RADARR-4K_APIKEY":                  "radarr-4k-api-key",
					"SEEDREAP_APPS_RADARR-4K_CATEGORY":                "movies-4k",
					"SEEDREAP_APPS_RADARR-4K_DOWNLOADSPATH":           "/data/downloads/movies-4k",
					"SEEDREAP_APPS_RADARR-4K_CLEANUPONCATEGORYCHANGE": "true",
					"SEEDREAP_APPS_RADARR-4K_CLEANUPONREMOVE":         "true",
					"SEEDREAP_APPS_MISC_TYPE":                         "passthrough",
					"SEEDREAP_APPS_MISC_CATEGORY":                     "misc",
				}

				for key, value := range envVars {
					t.Setenv(key, value)
				}

				// No config file - Load will use env vars
				return config.LoadOptions{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.setup(t)

			cfg, err := config.Load(opts)
			require.NoError(t, err, "failed to load config")

			// Server
			assert.Equal(t, "0.0.0.0:8080", cfg.Server.Listen)

			// Sync
			assert.Equal(t, "/data/downloads", cfg.Sync.DownloadsPath)
			assert.Equal(t, "/data/.seedreap-syncing", cfg.Sync.SyncingPath)
			assert.Equal(t, 4, cfg.Sync.MaxConcurrent)
			assert.Equal(t, 16, cfg.Sync.ParallelConnections)
			assert.Equal(t, 60*time.Second, cfg.Sync.PollInterval)
			assert.Equal(t, int64(52428800), cfg.Sync.TransferSpeedMax)

			// Downloader
			require.Len(t, cfg.Downloaders, 1)
			dl := cfg.Downloaders["seedbox"]
			assert.Equal(t, "qbittorrent", dl.Type)
			assert.Equal(t, "http://seedbox.example.com:8080", dl.URL)
			assert.Equal(t, "admin", dl.Username)
			assert.Equal(t, "secret123", dl.Password)
			assert.Equal(t, "seedbox.example.com", dl.SSH.Host)
			assert.Equal(t, 2222, dl.SSH.Port)
			assert.Equal(t, "seeduser", dl.SSH.User)
			assert.Equal(t, "/config/ssh/id_ed25519", dl.SSH.KeyFile)

			// Apps
			require.Len(t, cfg.Apps, 3)

			sonarr := cfg.Apps["sonarr"]
			assert.Equal(t, "sonarr", sonarr.Type)
			assert.Equal(t, "http://sonarr:8989", sonarr.URL)
			assert.Equal(t, "sonarr-api-key", sonarr.APIKey)
			assert.Equal(t, "tv-sonarr", sonarr.Category)
			assert.Empty(t, sonarr.DownloadsPath, "should be empty to use default path with downloader name")
			assert.True(t, sonarr.CleanupOnCategoryChange)
			assert.False(t, sonarr.CleanupOnRemove)

			radarr4k := cfg.Apps["radarr-4k"]
			assert.Equal(t, "radarr", radarr4k.Type)
			assert.Equal(t, "http://radarr-4k:7878", radarr4k.URL)
			assert.Equal(t, "radarr-4k-api-key", radarr4k.APIKey)
			assert.Equal(t, "movies-4k", radarr4k.Category)
			assert.Equal(t, "/data/downloads/movies-4k", radarr4k.DownloadsPath)
			assert.True(t, radarr4k.CleanupOnCategoryChange)
			assert.True(t, radarr4k.CleanupOnRemove)

			misc := cfg.Apps["misc"]
			assert.Equal(t, "passthrough", misc.Type)
			assert.Equal(t, "misc", misc.Category)
			assert.Empty(t, misc.URL)
			assert.Empty(t, misc.APIKey)
		})
	}
}

func TestEmptyMapsWhenNotConfigured(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
`
	cfg := loadConfigFromYAML(t, yaml)

	// Maps should be nil/empty when not configured
	assert.Empty(t, cfg.Downloaders)
	assert.Empty(t, cfg.Apps)
}

func TestLoadWithNoConfigFile(t *testing.T) {
	// When no config file exists and no env vars are set,
	// Load should return defaults without error
	cfg, err := config.Load(config.LoadOptions{})
	require.NoError(t, err)

	// Should have all defaults
	assert.Equal(t, "[::]:8423", cfg.Server.Listen)
	assert.Equal(t, "/downloads", cfg.Sync.DownloadsPath)
	assert.Equal(t, "/downloads/syncing", cfg.Sync.SyncingPath)
	assert.Equal(t, 2, cfg.Sync.MaxConcurrent)
	assert.Equal(t, 30*time.Second, cfg.Sync.PollInterval)
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		errContains string
	}{
		{
			name: "downloader missing type",
			yaml: `
downloaders:
  seedbox:
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": type is required`,
		},
		{
			name: "downloader missing url",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": url is required`,
		},
		{
			name: "downloader missing ssh host",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      user: seeduser
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": ssh.host is required`,
		},
		{
			name: "downloader missing ssh user",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      keyFile: /path/to/key
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": ssh.user is required`,
		},
		{
			name: "downloader missing ssh keyFile",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": ssh.keyFile is required`,
		},
		{
			name: "downloader missing ssh host key config",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
`,
			errContains: `downloader "seedbox": ssh.knownHostsFile is required (or set ssh.ignoreHostKey to true)`,
		},
		{
			name: "downloader ssh knownHostsFile and ignoreHostKey mutually exclusive",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
      knownHostsFile: /path/to/known_hosts
      ignoreHostKey: true
`,
			errContains: `downloader "seedbox": ssh.knownHostsFile and ssh.ignoreHostKey are mutually exclusive`,
		},
		{
			name: "app missing type",
			yaml: `
apps:
  sonarr:
    url: http://sonarr:8989
    apiKey: test-key
    category: tv
`,
			errContains: `app "sonarr": type is required`,
		},
		{
			name: "app missing category",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
`,
			errContains: `app "sonarr": category is required`,
		},
		{
			name: "app missing url (non-passthrough)",
			yaml: `
apps:
  sonarr:
    type: sonarr
    apiKey: test-key
    category: tv
`,
			errContains: `app "sonarr": url is required`,
		},
		{
			name: "app missing apiKey (non-passthrough)",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    category: tv
`,
			errContains: `app "sonarr": apiKey is required`,
		},
		{
			name: "passthrough app does not require url or apiKey",
			yaml: `
apps:
  media:
    type: passthrough
    category: media
`,
			errContains: "", // No error expected
		},
		{
			name: "downloader unknown type",
			yaml: `
downloaders:
  seedbox:
    type: unknown_type
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: /path/to/key
`,
			errContains: `downloader "seedbox": unknown type "unknown_type"`,
		},
		{
			name: "app unknown type",
			yaml: `
apps:
  myapp:
    type: unknown_type
    url: http://example.com
    apiKey: test-key
    category: test
`,
			errContains: `app "myapp": unknown type "unknown_type"`,
		},
		{
			name: "multiple validation errors",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
apps:
  sonarr:
    type: sonarr
    category: tv
`,
			errContains: "url is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configFile, []byte(tt.yaml), 0644)
			require.NoError(t, err)

			_, err = config.Load(config.LoadOptions{ConfigFile: configFile})

			if tt.errContains == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}
