//nolint:testpackage // tests access internal types
package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	testutil "github.com/seedreap/seedreap/internal/testing"
)

// loadConfigFromYAML creates a temp config file and loads it using config.Load().
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
