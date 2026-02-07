package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/ent/generated"
)

// qbittorrentClient implements the Client interface for qBittorrent.
// It is private and only exposed via the Client interface.
type qbittorrentClient struct {
	name       string
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	logger     zerolog.Logger
}

// qbittorrentAPITorrent represents a torrent from the qBittorrent API.
type qbittorrentAPITorrent struct {
	Hash           string  `json:"hash"`
	Name           string  `json:"name"`
	Category       string  `json:"category"`
	State          string  `json:"state"`
	SavePath       string  `json:"save_path"`
	ContentPath    string  `json:"content_path"`
	Size           int64   `json:"size"`
	Downloaded     int64   `json:"downloaded"`
	Progress       float64 `json:"progress"`
	AddedOn        int64   `json:"added_on"`
	CompletionOn   int64   `json:"completion_on"`
	AmountLeft     int64   `json:"amount_left"`
	TotalSize      int64   `json:"total_size"`
	DownloadedSize int64   `json:"downloaded_session"`
}

// qbittorrentAPIFile represents a file from the qBittorrent API.
type qbittorrentAPIFile struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	IsSeed       bool    `json:"is_seed"`
	PieceRange   []int   `json:"piece_range"`
	Availability float64 `json:"availability"`
}

// setLogger implements configurable for shared options.
func (c *qbittorrentClient) setLogger(logger zerolog.Logger) {
	c.logger = logger
}

// NewQBittorrent creates a new qBittorrent client and returns it as Client.
func NewQBittorrent(d *generated.DownloadClient, opts ...Option) Client {
	jar, _ := cookiejar.New(nil)

	c := &qbittorrentClient{
		name:     d.Name,
		baseURL:  strings.TrimSuffix(d.URL, "/"),
		username: d.Username,
		password: d.Password,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: time.Duration(d.HTTPTimeout) * time.Second,
		},
		logger: zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Name returns the configured name of this downloader instance.
func (c *qbittorrentClient) Name() string {
	return c.name
}

// Type returns the type of download.
func (c *qbittorrentClient) Type() string {
	return "qbittorrent"
}

// Connect establishes a connection to the qBittorrent API.
func (c *qbittorrentClient) Connect(ctx context.Context) error {
	if err := c.login(ctx); err != nil {
		return fmt.Errorf("qbittorrent login failed: %w", err)
	}

	c.logger.Info().
		Str("name", c.name).
		Str("url", c.baseURL).
		Msg("connected to qbittorrent")

	return nil
}

// Close closes all connections.
func (c *qbittorrentClient) Close() error {
	// HTTP client doesn't need explicit closing
	return nil
}

func (c *qbittorrentClient) login(ctx context.Context) error {
	// If no credentials provided, skip authentication
	// (qBittorrent may be configured without auth)
	if c.username == "" && c.password == "" {
		c.logger.Debug().Msg("no credentials provided, skipping authentication")

		// Verify we can access the API without auth
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/app/version", nil)
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden {
			return errors.New("authentication required but no credentials provided")
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to connect: status %d", resp.StatusCode)
		}

		return nil
	}

	data := url.Values{
		"username": {c.username},
		"password": {c.password},
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.baseURL+"/api/v2/auth/login",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "Ok." {
		return fmt.Errorf("login failed: %s", string(body))
	}

	return nil
}

// ListDownloads returns all torrents matching the given categories.
func (c *qbittorrentClient) ListDownloads(ctx context.Context, categories []string) ([]*Download, error) {
	params := url.Values{}
	if len(categories) == 1 {
		params.Set("category", categories[0])
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/torrents/info?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var torrents []qbittorrentAPITorrent
	if err = json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	// Filter by categories if multiple specified
	categorySet := make(map[string]bool)
	for _, cat := range categories {
		categorySet[cat] = true
	}

	var downloads []*Download
	for _, t := range torrents {
		if len(categories) > 1 && !categorySet[t.Category] {
			continue
		}
		downloads = append(downloads, c.toDownload(t))
	}

	return downloads, nil
}

// GetDownload returns a specific torrent by hash.
func (c *qbittorrentClient) GetDownload(ctx context.Context, id string) (*Download, error) {
	params := url.Values{
		"hashes": {id},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/torrents/info?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var torrents []qbittorrentAPITorrent
	if err = json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	if len(torrents) == 0 {
		return nil, fmt.Errorf("torrent not found: %s", id)
	}

	return c.toDownload(torrents[0]), nil
}

// GetFiles returns the files for a torrent.
func (c *qbittorrentClient) GetFiles(ctx context.Context, id string) ([]File, error) {
	params := url.Values{
		"hash": {id},
	}

	reqURL := c.baseURL + "/api/v2/torrents/files?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var files []qbittorrentAPIFile
	if err = json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}

	result := make([]File, len(files))
	for i, f := range files {
		var state FileState

		switch {
		case f.Priority == 0:
			state = FileStateSkipped
		case f.Progress >= 1.0:
			state = FileStateComplete
		default:
			state = FileStateDownloading
		}

		downloaded := int64(f.Progress * float64(f.Size))

		result[i] = File{
			Path:       f.Name,
			Size:       f.Size,
			Downloaded: downloaded,
			State:      state,
			Priority:   f.Priority,
		}
	}

	return result, nil
}

func (c *qbittorrentClient) toDownload(t qbittorrentAPITorrent) *Download {
	state := TorrentStateDownloading

	switch t.State {
	case "uploading", "stalledUP", "queuedUP", "forcedUP", "checkingUP":
		state = TorrentStateComplete
	case "pausedDL", "pausedUP", "stoppedDL", "stoppedUP":
		// qBittorrent v4.5+ renamed "paused" to "stopped"
		state = TorrentStatePaused
	case "error", "missingFiles":
		state = TorrentStateError
	}

	// If progress is 1.0, it's complete regardless of state string
	if t.Progress >= 1.0 {
		state = TorrentStateComplete
	}

	var addedOn, completedOn time.Time
	if t.AddedOn > 0 {
		addedOn = time.Unix(t.AddedOn, 0)
	}
	if t.CompletionOn > 0 {
		completedOn = time.Unix(t.CompletionOn, 0)
	}

	return &Download{
		ID:          t.Hash,
		Name:        t.Name,
		Hash:        t.Hash,
		Category:    t.Category,
		State:       state,
		SavePath:    t.SavePath,
		ContentPath: t.ContentPath,
		Size:        t.Size,
		Downloaded:  t.Downloaded,
		Progress:    t.Progress,
		AddedOn:     addedOn,
		CompletedOn: completedOn,
	}
}
