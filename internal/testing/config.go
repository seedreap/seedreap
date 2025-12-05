package testing

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/seedreap/seedreap/internal/config"
)

// TestSSHFiles holds paths to test SSH files created by CreateTestSSHFiles.
type TestSSHFiles struct {
	KeyFile        string
	KnownHostsFile string
	TempDir        string
}

// CreateTestSSHFiles creates mock SSH key and known_hosts files for testing.
// The key file is created with 0600 permissions as required by validation.
// Call t.Cleanup to ensure files are removed when the test completes.
func CreateTestSSHFiles(t *testing.T) TestSSHFiles {
	t.Helper()

	tmpDir := t.TempDir()

	// Create a mock SSH key file with secure permissions
	keyFile := filepath.Join(tmpDir, "id_test")
	keyContent := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to create test key file: %v", err)
	}

	// Create a mock known_hosts file
	knownHostsFile := filepath.Join(tmpDir, "known_hosts")
	if err := os.WriteFile(knownHostsFile, []byte("# test known_hosts\n"), 0600); err != nil {
		t.Fatalf("failed to create test known_hosts file: %v", err)
	}

	return TestSSHFiles{
		KeyFile:        keyFile,
		KnownHostsFile: knownHostsFile,
		TempDir:        tmpDir,
	}
}

// ValidConfig returns a fully populated, valid config.Config struct.
// The returned config passes all validation checks and can be used as a starting
// point for tests that need to modify specific fields.
//
// SSH files are created automatically and cleaned up when the test completes.
func ValidConfig(t *testing.T) config.Config {
	t.Helper()

	sshFiles := CreateTestSSHFiles(t)

	return config.Config{
		Server: config.ServerConfig{
			Listen: "[::]:8423",
		},
		Downloaders: map[string]config.DownloaderConfig{
			"seedbox": {
				Type:        "qbittorrent",
				URL:         "http://seedbox.example.com:8080",
				Username:    "admin",
				Password:    "secret",
				HTTPTimeout: config.DefaultHTTPTimeout,
				SSH: config.SSHConfig{
					Host:          "seedbox.example.com",
					Port:          config.DefaultSSHPort,
					User:          "seeduser",
					KeyFile:       sshFiles.KeyFile,
					IgnoreHostKey: true,
					Timeout:       config.DefaultSSHTimeout,
				},
			},
		},
		Apps: map[string]config.AppEntryConfig{
			"sonarr": {
				Type:        "sonarr",
				URL:         "http://sonarr:8989",
				APIKey:      "test-api-key",
				Category:    "tv-sonarr",
				HTTPTimeout: config.DefaultHTTPTimeout,
			},
		},
		Sync: config.SyncConfig{
			DownloadsPath: "/downloads",
			SyncingPath:   "/downloads/syncing",
			MaxConcurrent: config.DefaultMaxConcurrent,
			PollInterval:  config.DefaultHTTPTimeout, // Same default of 30s
		},
	}
}

// ValidConfigWithKnownHosts returns a valid config using a known_hosts file
// instead of ignoreHostKey.
func ValidConfigWithKnownHosts(t *testing.T) config.Config {
	t.Helper()

	cfg := ValidConfig(t)
	sshFiles := CreateTestSSHFiles(t)

	// Update the downloader to use known_hosts
	dl := cfg.Downloaders["seedbox"]
	dl.SSH.IgnoreHostKey = false
	dl.SSH.KnownHostsFile = sshFiles.KnownHostsFile
	cfg.Downloaders["seedbox"] = dl

	return cfg
}

// ValidConfigMinimal returns a minimal valid config with only required fields.
func ValidConfigMinimal(t *testing.T) config.Config {
	t.Helper()

	sshFiles := CreateTestSSHFiles(t)

	return config.Config{
		Server: config.ServerConfig{
			Listen: "[::]:8423",
		},
		Downloaders: map[string]config.DownloaderConfig{
			"seedbox": {
				Type: "qbittorrent",
				URL:  "http://seedbox:8080",
				SSH: config.SSHConfig{
					Host:          "seedbox.example.com",
					User:          "seeduser",
					KeyFile:       sshFiles.KeyFile,
					IgnoreHostKey: true,
				},
			},
		},
		Apps: map[string]config.AppEntryConfig{
			"misc": {
				Type:     "passthrough",
				Category: "misc",
			},
		},
		Sync: config.SyncConfig{
			DownloadsPath: "/downloads",
			SyncingPath:   "/downloads/syncing",
		},
	}
}

// ConfigToYAML converts a config.Config struct to a YAML string.
// This is useful for tests that need to load config via the YAML parser.
// Note: config.Config uses mapstructure tags which yaml.Marshal handles correctly.
func ConfigToYAML(t *testing.T, cfg config.Config) string {
	t.Helper()

	//nolint:musttag // config.Config uses mapstructure tags, yaml.Marshal uses field names
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config to YAML: %v", err)
	}

	return string(data)
}
