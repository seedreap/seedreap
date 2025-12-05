package testing_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	testutil "github.com/seedreap/seedreap/internal/testing"
)

func TestValidConfig(t *testing.T) {
	cfg := testutil.ValidConfig(t)

	// Write the config to a temp file and load it to verify it's valid
	yamlContent := testutil.ConfigToYAML(t, cfg)
	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(yamlContent), 0600))

	loaded, err := config.Load(config.LoadOptions{ConfigFile: tmpFile})
	require.NoError(t, err, "ValidConfig should produce a valid config")

	// Verify key fields are present
	assert.NotEmpty(t, loaded.Server.Listen)
	assert.NotEmpty(t, loaded.Downloaders)
	assert.NotEmpty(t, loaded.Apps)
	assert.NotEmpty(t, loaded.Sync.DownloadsPath)
	assert.NotEmpty(t, loaded.Sync.SyncingPath)

	// Verify downloader has required fields
	dl, ok := loaded.Downloaders["seedbox"]
	require.True(t, ok, "seedbox downloader should exist")
	assert.Equal(t, "qbittorrent", dl.Type)
	assert.NotEmpty(t, dl.URL)
	assert.NotEmpty(t, dl.SSH.Host)
	assert.NotEmpty(t, dl.SSH.User)
	assert.NotEmpty(t, dl.SSH.KeyFile)

	// Verify app has required fields
	app, ok := loaded.Apps["sonarr"]
	require.True(t, ok, "sonarr app should exist")
	assert.Equal(t, "sonarr", app.Type)
	assert.NotEmpty(t, app.URL)
	assert.NotEmpty(t, app.APIKey)
	assert.NotEmpty(t, app.Category)
}

func TestValidConfigWithKnownHosts(t *testing.T) {
	cfg := testutil.ValidConfigWithKnownHosts(t)

	// Write the config to a temp file and load it to verify it's valid
	yamlContent := testutil.ConfigToYAML(t, cfg)
	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(yamlContent), 0600))

	loaded, err := config.Load(config.LoadOptions{ConfigFile: tmpFile})
	require.NoError(t, err, "ValidConfigWithKnownHosts should produce a valid config")

	// Verify SSH uses known_hosts instead of ignoreHostKey
	dl := loaded.Downloaders["seedbox"]
	assert.False(t, dl.SSH.IgnoreHostKey)
	assert.NotEmpty(t, dl.SSH.KnownHostsFile)
}

func TestValidConfigMinimal(t *testing.T) {
	cfg := testutil.ValidConfigMinimal(t)

	// Write the config to a temp file and load it to verify it's valid
	yamlContent := testutil.ConfigToYAML(t, cfg)
	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(yamlContent), 0600))

	loaded, err := config.Load(config.LoadOptions{ConfigFile: tmpFile})
	require.NoError(t, err, "ValidConfigMinimal should produce a valid config")

	// Verify minimal config has only required fields
	assert.NotEmpty(t, loaded.Server.Listen)
	assert.Len(t, loaded.Downloaders, 1)
	assert.Len(t, loaded.Apps, 1)

	// Verify passthrough app doesn't require URL or API key
	app := loaded.Apps["misc"]
	assert.Equal(t, "passthrough", app.Type)
	assert.Empty(t, app.URL)
	assert.Empty(t, app.APIKey)
}

func TestCreateTestSSHFiles(t *testing.T) {
	files := testutil.CreateTestSSHFiles(t)

	// Verify key file exists with correct permissions
	info, err := os.Stat(files.KeyFile)
	require.NoError(t, err, "key file should exist")
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "key file should have 0600 permissions")

	// Verify known_hosts file exists
	_, err = os.Stat(files.KnownHostsFile)
	require.NoError(t, err, "known_hosts file should exist")

	// Verify temp dir is the parent
	assert.Equal(t, files.TempDir, filepath.Dir(files.KeyFile))
	assert.Equal(t, files.TempDir, filepath.Dir(files.KnownHostsFile))
}

func TestConfigToYAML(t *testing.T) {
	cfg := testutil.ValidConfig(t)
	yamlContent := testutil.ConfigToYAML(t, cfg)

	// Verify YAML contains expected keys
	assert.Contains(t, yamlContent, "server:")
	assert.Contains(t, yamlContent, "downloaders:")
	assert.Contains(t, yamlContent, "apps:")
	assert.Contains(t, yamlContent, "sync:")
	assert.Contains(t, yamlContent, "seedbox:")
	assert.Contains(t, yamlContent, "sonarr:")
}
