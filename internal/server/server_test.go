//nolint:testpackage // tests access internal types
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadclient"
	testutil "github.com/seedreap/seedreap/internal/testing"
)

// loadConfigFromYAML creates a temp config file and loads it using config.Load().
// This ensures tests use the exact same config loading code as the application.
// Each test gets an isolated in-memory database to prevent state leaking between tests.
func loadConfigFromYAML(t *testing.T, yaml string) config.Config {
	t.Helper()

	// Add isolated database config for tests (non-shared in-memory database)
	// This ensures each test gets its own database instance
	if !strings.Contains(yaml, "database:") {
		yaml = "database:\n  dsn: \":memory:\"\n" + yaml
	}

	// Create temp directory for config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write YAML to temp file
	err := os.WriteFile(configFile, []byte(yaml), 0600)
	require.NoError(t, err, "failed to write temp config file")

	// Load using the same function the app uses
	cfg, err := config.Load(config.LoadOptions{ConfigFile: configFile})
	require.NoError(t, err, "failed to load config")

	return cfg
}

// loadConfigFromYAMLWithSSH creates temp SSH files and substitutes placeholders in the YAML.
// Placeholders: {{KEY_FILE}} and {{KNOWN_HOSTS_FILE}}.
func loadConfigFromYAMLWithSSH(t *testing.T, yaml string) config.Config {
	t.Helper()

	sshFiles := testutil.CreateTestSSHFiles(t)

	// Replace placeholders with actual paths
	yaml = strings.ReplaceAll(yaml, "{{KEY_FILE}}", sshFiles.KeyFile)
	yaml = strings.ReplaceAll(yaml, "{{KNOWN_HOSTS_FILE}}", sshFiles.KnownHostsFile)

	return loadConfigFromYAML(t, yaml)
}

func TestServerNew_DefaultsApplied(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, srv *Server, cfg config.Config)
	}{
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
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
`,
			check: func(t *testing.T, _ *Server, cfg config.Config) {
				// config.Load() applies the default of 22
				assert.Equal(t, config.DefaultSSHPort, cfg.Downloaders["seedbox"].SSH.Port)
			},
		},
		{
			name: "ssh port is used when specified",
			yaml: `
downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      port: 2222
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
`,
			check: func(t *testing.T, _ *Server, cfg config.Config) {
				assert.Equal(t, 2222, cfg.Downloaders["seedbox"].SSH.Port)
			},
		},
		{
			name: "maxConcurrent defaults to 2",
			yaml: `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
`,
			check: func(t *testing.T, _ *Server, cfg config.Config) {
				assert.Equal(t, 2, cfg.Sync.MaxConcurrent)
			},
		},
		{
			name: "parallelConnections uses config value when set",
			yaml: `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  parallelConnections: 16
`,
			check: func(t *testing.T, _ *Server, cfg config.Config) {
				assert.Equal(t, 16, cfg.Sync.ParallelConnections)
			},
		},
		{
			name: "pollInterval defaults to 30s",
			yaml: `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
`,
			check: func(t *testing.T, _ *Server, cfg config.Config) {
				assert.Equal(t, 30*time.Second, cfg.Sync.PollInterval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAMLWithSSH(t, tt.yaml)

			opts := Options{
				Logger: zerolog.Nop(),
			}

			srv, err := New(cfg, opts)
			require.NoError(t, err)
			require.NotNil(t, srv)

			tt.check(t, srv, cfg)
		})
	}
}

func TestServerNew_AppDownloadsPathNotPrecomputed(t *testing.T) {
	// This test verifies the fix for the bug where downloadsPath was
	// pre-computed without the downloader name. The app's DownloadsPath
	// should remain empty when not explicitly set, so the orchestrator
	// can compute the correct path: {downloadsPath}/{downloader_name}/{category}

	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
    category: tv-sonarr
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	// Verify the config has empty downloadsPath for the app
	assert.Empty(t, cfg.Apps["sonarr"].DownloadsPath,
		"app downloadsPath should be empty in config so orchestrator can compute path with downloader name")

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// The server should NOT pre-compute a downloads path
	// This is verified by the config staying empty
}

func TestServerNew_AppDownloadsPathExplicitlySet(t *testing.T) {
	// When downloadsPath is explicitly set, it should be used as-is

	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
    category: tv-sonarr
    downloadsPath: /custom/tv/path
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	assert.Equal(t, "/custom/tv/path", cfg.Apps["sonarr"].DownloadsPath,
		"explicit downloadsPath should be preserved")

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestServerNew_MultipleAppsAndDownloaders(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox1:
    type: qbittorrent
    url: http://seedbox1:8080
    ssh:
      host: seedbox1.example.com
      user: user1
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
  seedbox2:
    type: qbittorrent
    url: http://seedbox2:8080
    ssh:
      host: seedbox2.example.com
      user: user2
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: sonarr-key
    category: tv-sonarr
    cleanupOnCategoryChange: true
  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: radarr-key
    category: movies-radarr
  misc:
    type: passthrough
    category: misc
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify config was parsed correctly
	assert.Len(t, cfg.Downloaders, 2)
	assert.Len(t, cfg.Apps, 3)

	// All app downloadsPath should be empty (not pre-computed)
	assert.Empty(t, cfg.Apps["sonarr"].DownloadsPath)
	assert.Empty(t, cfg.Apps["radarr"].DownloadsPath)
	assert.Empty(t, cfg.Apps["misc"].DownloadsPath)
}

func TestServerNew_CleanupOptions(t *testing.T) {
	tests := []struct {
		name                    string
		yaml                    string
		expectedCleanupCategory bool
		expectedCleanupRemove   bool
	}{
		{
			name: "defaults to false",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: key
    category: tv
`,
			expectedCleanupCategory: false,
			expectedCleanupRemove:   false,
		},
		{
			name: "cleanupOnCategoryChange enabled",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: key
    category: tv
    cleanupOnCategoryChange: true
`,
			expectedCleanupCategory: true,
			expectedCleanupRemove:   false,
		},
		{
			name: "cleanupOnRemove enabled",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: key
    category: tv
    cleanupOnRemove: true
`,
			expectedCleanupCategory: false,
			expectedCleanupRemove:   true,
		},
		{
			name: "both cleanup options enabled",
			yaml: `
apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: key
    category: tv
    cleanupOnCategoryChange: true
    cleanupOnRemove: true
`,
			expectedCleanupCategory: true,
			expectedCleanupRemove:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAMLWithSSH(t, tt.yaml)

			app := cfg.Apps["sonarr"]
			assert.Equal(t, tt.expectedCleanupCategory, app.CleanupOnCategoryChange)
			assert.Equal(t, tt.expectedCleanupRemove, app.CleanupOnRemove)

			opts := Options{
				Logger: zerolog.Nop(),
			}

			srv, err := New(cfg, opts)
			require.NoError(t, err)
			require.NotNil(t, srv)
		})
	}
}

func TestServerNew_UnknownDownloaderType(t *testing.T) {
	sshFiles := testutil.CreateTestSSHFiles(t)

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	yaml := `
downloaders:
  unknown:
    type: unknown_type
    url: http://example.com
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: ` + sshFiles.KeyFile + `
      ignoreHostKey: true
`
	err := os.WriteFile(configFile, []byte(yaml), 0644)
	require.NoError(t, err)

	// Config validation should fail for unknown type
	_, err = config.Load(config.LoadOptions{ConfigFile: configFile})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown type "unknown_type"`)
}

func TestServerNew_UnknownAppType(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	yaml := `
apps:
  unknown:
    type: unknown_type
    url: http://example.com
    apiKey: test-key
    category: test
`
	err := os.WriteFile(configFile, []byte(yaml), 0644)
	require.NoError(t, err)

	// Config validation should fail for unknown type
	_, err = config.Load(config.LoadOptions{ConfigFile: configFile})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown type "unknown_type"`)
}

func TestServerNew_NoAppsConfigured(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	// Server should start successfully with no apps (just logs a warning)
	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestServerNew_NoDownloadersConfigured(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
    category: tv-sonarr
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	// Server should start successfully with no downloaders
	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestServerNew_PassthroughApp(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true

apps:
  misc:
    type: passthrough
    category: misc
    downloadsPath: /downloads/misc
    cleanupOnCategoryChange: true
    cleanupOnRemove: true
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify passthrough app config was parsed
	assert.Equal(t, "passthrough", cfg.Apps["misc"].Type)
	assert.Equal(t, "misc", cfg.Apps["misc"].Category)
	assert.Equal(t, "/downloads/misc", cfg.Apps["misc"].DownloadsPath)
	assert.True(t, cfg.Apps["misc"].CleanupOnCategoryChange)
	assert.True(t, cfg.Apps["misc"].CleanupOnRemove)
}

func TestServerNew_DefaultParallelConnections(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// parallelConnections should default to 8 when not specified
	// This is applied in New(), not in config loading
	assert.Equal(t, 0, cfg.Sync.ParallelConnections, "config should have 0 (unset)")
	// The default of 8 is applied internally in New()
}

func TestServerNew_TransferSpeedLimit(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  transferSpeedMax: 10485760

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify speed limit was parsed
	assert.Equal(t, int64(10485760), cfg.Sync.TransferSpeedMax)
}

func TestServerNew_NoSSHConfig(t *testing.T) {
	// When no downloaders have SSH config, server should still work
	// (no transfer backend configured)
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

apps:
  misc:
    type: passthrough
    category: misc
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestServerNew_AllAppTypes(t *testing.T) {
	// Test all supported app types are correctly handled
	tests := []struct {
		name    string
		appType string
		yaml    string
	}{
		{
			name:    "sonarr",
			appType: "sonarr",
			yaml: `
apps:
  test:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
    category: tv
`,
		},
		{
			name:    "radarr",
			appType: "radarr",
			yaml: `
apps:
  test:
    type: radarr
    url: http://radarr:7878
    apiKey: test-key
    category: movies
`,
		},
		{
			name:    "passthrough",
			appType: "passthrough",
			yaml: `
apps:
  test:
    type: passthrough
    category: misc
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadConfigFromYAMLWithSSH(t, tt.yaml)

			opts := Options{
				Logger: zerolog.Nop(),
			}

			srv, err := New(cfg, opts)
			require.NoError(t, err)
			require.NotNil(t, srv)

			assert.Equal(t, tt.appType, cfg.Apps["test"].Type)
		})
	}
}

func TestServerNew_CompleteConfiguration(t *testing.T) {
	// Test a complete, realistic configuration
	yaml := `
server:
  listen: "[::]:8423"

sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
  maxConcurrent: 4
  parallelConnections: 16
  pollInterval: 60s
  transferSpeedMax: 52428800

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    username: admin
    password: secret
    ssh:
      host: seedbox.example.com
      port: 2222
      user: seeduser
      keyFile: {{KEY_FILE}}
      knownHostsFile: {{KNOWN_HOSTS_FILE}}

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: sonarr-key
    category: tv-sonarr
    cleanupOnCategoryChange: true
  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: radarr-key
    category: movies-radarr
    cleanupOnRemove: true
  misc:
    type: passthrough
    category: misc
    downloadsPath: /downloads/misc
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify server config
	assert.Equal(t, "[::]:8423", cfg.Server.Listen)

	// Verify sync config
	assert.Equal(t, "/downloads", cfg.Sync.DownloadsPath)
	assert.Equal(t, "/downloads/syncing", cfg.Sync.SyncingPath)
	assert.Equal(t, 4, cfg.Sync.MaxConcurrent)
	assert.Equal(t, 16, cfg.Sync.ParallelConnections)
	assert.Equal(t, 60*time.Second, cfg.Sync.PollInterval)
	assert.Equal(t, int64(52428800), cfg.Sync.TransferSpeedMax)

	// Verify downloader config
	assert.Len(t, cfg.Downloaders, 1)
	assert.Equal(t, "qbittorrent", cfg.Downloaders["seedbox"].Type)
	assert.Equal(t, "http://seedbox:8080", cfg.Downloaders["seedbox"].URL)
	assert.Equal(t, 2222, cfg.Downloaders["seedbox"].SSH.Port)

	// Verify apps config
	assert.Len(t, cfg.Apps, 3)
	assert.True(t, cfg.Apps["sonarr"].CleanupOnCategoryChange)
	assert.False(t, cfg.Apps["sonarr"].CleanupOnRemove)
	assert.False(t, cfg.Apps["radarr"].CleanupOnCategoryChange)
	assert.True(t, cfg.Apps["radarr"].CleanupOnRemove)
}

func TestServerNew_InitializesStoreAndEventBus(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify database is initialized
	assert.NotNil(t, srv.DB())

	// Verify event bus is initialized
	assert.NotNil(t, srv.EventBus())
	assert.Equal(t, 0, srv.EventBus().SubscriberCount())
}

func TestServerNew_LoadsDownloadersIntoDatabase(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox1:
    type: qbittorrent
    url: http://seedbox1:8080
    username: admin
    password: secret
    httpTimeout: 60s
    ssh:
      host: seedbox1.example.com
      port: 2222
      user: user1
      keyFile: {{KEY_FILE}}
      knownHostsFile: {{KNOWN_HOSTS_FILE}}
      timeout: 30s
  seedbox2:
    type: qbittorrent
    url: http://seedbox2:8080
    ssh:
      host: seedbox2.example.com
      user: user2
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify downloaders are in the database
	downloaders, err := srv.DB().DownloadClient.Query().All(context.Background())
	require.NoError(t, err)
	assert.Len(t, downloaders, 2)

	// Find seedbox1 and verify its fields
	var seedbox1 *generated.DownloadClient
	for _, d := range downloaders {
		if d.Name == "seedbox1" {
			seedbox1 = d
			break
		}
	}
	require.NotNil(t, seedbox1, "seedbox1 not found in database")
	assert.Equal(t, "qbittorrent", seedbox1.Type)
	assert.Equal(t, "http://seedbox1:8080", seedbox1.URL)
	assert.Equal(t, "admin", seedbox1.Username)
	assert.Equal(t, "secret", seedbox1.Password)
	assert.Equal(t, int64(60), seedbox1.HTTPTimeout)
	assert.True(t, seedbox1.Enabled)
	assert.Equal(t, "seedbox1.example.com", seedbox1.SSHHost)
	assert.Equal(t, 2222, seedbox1.SSHPort)
	assert.Equal(t, "user1", seedbox1.SSHUser)
	assert.False(t, seedbox1.SSHIgnoreHostKey)
	assert.Equal(t, int64(30), seedbox1.SSHTimeout)

	// Find seedbox2 and verify ignoreHostKey
	var seedbox2 *generated.DownloadClient
	for _, d := range downloaders {
		if d.Name == "seedbox2" {
			seedbox2 = d
			break
		}
	}
	require.NotNil(t, seedbox2, "seedbox2 not found in database")
	assert.True(t, seedbox2.SSHIgnoreHostKey)
	assert.Empty(t, seedbox2.SSHKnownHostsFile)
}

func TestServerNew_LoadsAppsIntoDatabase(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: sonarr-key
    category: tv-sonarr
    downloadsPath: /custom/tv
    httpTimeout: 45s
    cleanupOnCategoryChange: true
    cleanupOnRemove: false
  radarr:
    type: radarr
    url: http://radarr:7878
    apiKey: radarr-key
    category: movies-radarr
    cleanupOnCategoryChange: false
    cleanupOnRemove: true
  misc:
    type: passthrough
    category: misc
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify apps are in the database
	apps, err := srv.DB().App.Query().All(context.Background())
	require.NoError(t, err)
	assert.Len(t, apps, 3)

	// Find sonarr and verify its fields
	var sonarr *generated.App
	for _, a := range apps {
		if a.Name == "sonarr" {
			sonarr = a
			break
		}
	}
	require.NotNil(t, sonarr, "sonarr not found in database")
	assert.Equal(t, app.TypeSonarr, sonarr.Type)
	assert.Equal(t, "http://sonarr:8989", sonarr.URL)
	assert.Equal(t, "sonarr-key", sonarr.APIKey)
	assert.Equal(t, "tv-sonarr", sonarr.Category)
	assert.Equal(t, "/custom/tv", sonarr.DownloadsPath)
	assert.Equal(t, int64(45), sonarr.HTTPTimeout)
	assert.True(t, sonarr.Enabled)
	assert.True(t, sonarr.CleanupOnCategoryChange)
	assert.False(t, sonarr.CleanupOnRemove)

	// Find radarr and verify cleanup options
	var radarr *generated.App
	for _, a := range apps {
		if a.Name == "radarr" {
			radarr = a
			break
		}
	}
	require.NotNil(t, radarr, "radarr not found in database")
	assert.False(t, radarr.CleanupOnCategoryChange)
	assert.True(t, radarr.CleanupOnRemove)

	// Find misc (passthrough) and verify
	var misc *generated.App
	for _, a := range apps {
		if a.Name == "misc" {
			misc = a
			break
		}
	}
	require.NotNil(t, misc, "misc not found in database")
	assert.Equal(t, app.TypePassthrough, misc.Type)
	assert.Equal(t, "misc", misc.Category)
	assert.Empty(t, misc.URL)
	assert.Empty(t, misc.APIKey)
}

func TestServerNew_EmptyConfigLoadsEmptyDatabase(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Verify no downloaders in database
	downloaders, err := srv.DB().DownloadClient.Query().All(context.Background())
	require.NoError(t, err)
	assert.Empty(t, downloaders)

	// Verify no apps in database
	apps, err := srv.DB().App.Query().All(context.Background())
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestServerNew_ListEnabledFromDatabase(t *testing.T) {
	yaml := `
sync:
  downloadsPath: /downloads
  syncingPath: /downloads/syncing

downloaders:
  seedbox:
    type: qbittorrent
    url: http://seedbox:8080
    ssh:
      host: seedbox.example.com
      user: seeduser
      keyFile: {{KEY_FILE}}
      ignoreHostKey: true

apps:
  sonarr:
    type: sonarr
    url: http://sonarr:8989
    apiKey: test-key
    category: tv-sonarr
`

	cfg := loadConfigFromYAMLWithSSH(t, yaml)

	opts := Options{
		Logger: zerolog.Nop(),
	}

	srv, err := New(cfg, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// All items should be enabled by default
	enabledDownloaders, err := srv.DB().DownloadClient.Query().
		Where(downloadclient.EnabledEQ(true)).
		All(context.Background())
	require.NoError(t, err)
	assert.Len(t, enabledDownloaders, 1)

	enabledApps, err := srv.DB().App.Query().
		Where(app.EnabledEQ(true)).
		All(context.Background())
	require.NoError(t, err)
	assert.Len(t, enabledApps, 1)
}
