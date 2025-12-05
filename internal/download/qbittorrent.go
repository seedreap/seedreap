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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"

	"github.com/seedreap/seedreap/internal/config"
)

// qbittorrentClient implements the Downloader interface for qBittorrent.
// It is private and only exposed via the Downloader interface.
type qbittorrentClient struct {
	name       string
	baseURL    string
	username   string
	password   string
	sshConfig  config.SSHConfig
	httpClient *http.Client
	sshClient  *ssh.Client
	sftpClient *sftp.Client
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

// NewQBittorrent creates a new qBittorrent client and returns it as Downloader.
func NewQBittorrent(name string, cfg config.DownloaderConfig, opts ...Option) Downloader {
	jar, _ := cookiejar.New(nil)

	c := &qbittorrentClient{
		name:      name,
		baseURL:   strings.TrimSuffix(cfg.URL, "/"),
		username:  cfg.Username,
		password:  cfg.Password,
		sshConfig: cfg.SSH,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: cfg.HTTPTimeout,
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

// SSHConfig returns the SSH configuration.
func (c *qbittorrentClient) SSHConfig() config.SSHConfig {
	return c.sshConfig
}

// Connect establishes connections to qBittorrent API and SSH.
func (c *qbittorrentClient) Connect(ctx context.Context) error {
	// Login to qBittorrent API
	if err := c.login(ctx); err != nil {
		return fmt.Errorf("qbittorrent login failed: %w", err)
	}

	// Connect SSH/SFTP
	if err := c.connectSSH(); err != nil {
		return fmt.Errorf("ssh connection failed: %w", err)
	}

	c.logger.Info().
		Str("name", c.name).
		Str("url", c.baseURL).
		Msg("connected to qbittorrent")

	return nil
}

// Close closes all connections.
func (c *qbittorrentClient) Close() error {
	var errs []error
	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing sftp client: %w", err))
		}
	}
	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing ssh client: %w", err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
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

func (c *qbittorrentClient) connectSSH() error {
	if c.sshConfig.Host == "" {
		return errors.New("ssh host not configured")
	}

	port := c.sshConfig.Port
	if port == 0 {
		port = 22
	}

	keyBytes, err := os.ReadFile(c.sshConfig.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to read ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return fmt.Errorf("failed to parse ssh key: %w", err)
	}

	sshClientConfig := &ssh.ClientConfig{
		User: c.sshConfig.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // TODO: implement known_hosts verification
		Timeout:         c.sshConfig.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", c.sshConfig.Host, port)
	c.sshClient, err = ssh.Dial("tcp", addr, sshClientConfig)
	if err != nil {
		return fmt.Errorf("failed to dial ssh: %w", err)
	}

	c.sftpClient, err = sftp.NewClient(c.sshClient)
	if err != nil {
		return fmt.Errorf("failed to create sftp client: %w", err)
	}

	c.logger.Debug().
		Str("host", c.sshConfig.Host).
		Int("port", port).
		Str("user", c.sshConfig.User).
		Msg("ssh/sftp connected")

	return nil
}

// ListDownloads returns all torrents matching the given categories.
func (c *qbittorrentClient) ListDownloads(ctx context.Context, categories []string) ([]Download, error) {
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

	var downloads []Download
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

	d := c.toDownload(torrents[0])
	return &d, nil
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
		downloaded := int64(float64(f.Size) * f.Progress)
		state := FileStateDownloading
		if f.Progress >= 1.0 {
			state = FileStateComplete
		}

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

// OpenFile opens a remote file for reading.
func (c *qbittorrentClient) OpenFile(_ context.Context, path string) (io.ReadCloser, error) {
	if c.sftpClient == nil {
		return nil, errors.New("sftp client not connected")
	}

	// Clean the path
	path = filepath.Clean(path)

	f, err := c.sftpClient.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open remote file %s: %w", path, err)
	}

	return f, nil
}

func (c *qbittorrentClient) toDownload(t qbittorrentAPITorrent) Download {
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

	return Download{
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
