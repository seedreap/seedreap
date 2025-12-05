package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// arrClient implements the App interface for *arr applications (Sonarr, Radarr, etc).
// It is private and only exposed via the App interface.
type arrClient struct {
	name                    string
	appType                 string
	scanCommand             string
	baseURL                 string
	apiKey                  string
	category                string
	downloadsPath           string
	cleanupOnCategoryChange bool
	cleanupOnRemove         bool
	httpClient              *http.Client
	logger                  zerolog.Logger
}

// arrCommandRequest represents a command request to the *arr API.
type arrCommandRequest struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// arrSystemStatus represents the response from the system/status endpoint.
type arrSystemStatus struct {
	Version string `json:"version"`
	AppName string `json:"appName"`
}

// ArrConfig holds configuration for an *arr app client.
type ArrConfig struct {
	URL           string
	APIKey        string
	Category      string
	DownloadsPath string
	HTTPTimeout   time.Duration
}

// setLogger implements configurable for shared options.
func (c *arrClient) setLogger(logger zerolog.Logger) {
	c.logger = logger
}

// setCleanupOnCategoryChange implements configurable for shared options.
func (c *arrClient) setCleanupOnCategoryChange(cleanup bool) {
	c.cleanupOnCategoryChange = cleanup
}

// setCleanupOnRemove implements configurable for shared options.
func (c *arrClient) setCleanupOnRemove(cleanup bool) {
	c.cleanupOnRemove = cleanup
}

// newArrClient creates a new *arr client.
func newArrClient(name, appType, scanCommand string, cfg ArrConfig, opts ...Option) App {
	c := &arrClient{
		name:          name,
		appType:       appType,
		scanCommand:   scanCommand,
		baseURL:       strings.TrimSuffix(cfg.URL, "/"),
		apiKey:        cfg.APIKey,
		category:      cfg.Category,
		downloadsPath: cfg.DownloadsPath,
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		logger: zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// NewSonarr creates a new Sonarr client and returns it as App.
func NewSonarr(name string, cfg ArrConfig, opts ...Option) App {
	return newArrClient(name, "sonarr", "DownloadedEpisodesScan", cfg, opts...)
}

// NewRadarr creates a new Radarr client and returns it as App.
func NewRadarr(name string, cfg ArrConfig, opts ...Option) App {
	return newArrClient(name, "radarr", "DownloadedMoviesScan", cfg, opts...)
}

// Name returns the configured name of this app instance.
func (c *arrClient) Name() string {
	return c.name
}

// Type returns the type of app.
func (c *arrClient) Type() string {
	return c.appType
}

// Category returns the download category this app handles.
func (c *arrClient) Category() string {
	return c.category
}

// DownloadsPath returns the path where completed downloads should be placed.
func (c *arrClient) DownloadsPath() string {
	return c.downloadsPath
}

// CleanupOnCategoryChange returns true if synced files should be deleted when
// the download's category changes in the downloader.
func (c *arrClient) CleanupOnCategoryChange() bool {
	return c.cleanupOnCategoryChange
}

// CleanupOnRemove returns true if synced files should be deleted when the
// download is removed from the downloader.
func (c *arrClient) CleanupOnRemove() bool {
	return c.cleanupOnRemove
}

// TriggerImport tells the *arr app to scan for and import completed downloads.
func (c *arrClient) TriggerImport(ctx context.Context, path string) error {
	cmd := arrCommandRequest{
		Name: c.scanCommand,
	}
	if path != "" {
		cmd.Path = path
	}

	body, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v3/command", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to trigger import: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned status %d: %s", c.appType, resp.StatusCode, string(respBody))
	}

	c.logger.Info().
		Str("name", c.name).
		Str("path", path).
		Msgf("triggered %s import scan", c.appType)

	return nil
}

// TestConnection tests the connection to the *arr app.
func (c *arrClient) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v3/system/status", nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.appType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", c.appType, resp.StatusCode)
	}

	var status arrSystemStatus
	if err = json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Debug().
		Str("name", c.name).
		Str("version", status.Version).
		Str("app", status.AppName).
		Msgf("%s connection test successful", c.appType)

	return nil
}
