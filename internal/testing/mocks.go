// Package testing provides mock implementations for use in tests.
// This package should only be imported by test files (*_test.go).
package testing

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/transfer"
)

// ErrNotFound is returned when a download is not found.
var ErrNotFound = errors.New("download not found")

// MockDownloader is a mock implementation of download.Downloader for testing.
type MockDownloader struct {
	name      string
	dlType    string
	sshConfig config.SSHConfig

	mu        sync.RWMutex
	downloads map[string]*download.Download
	files     map[string][]download.File

	// Hooks for custom behavior
	OnConnect       func(ctx context.Context) error
	OnListDownloads func(ctx context.Context, categories []string) ([]download.Download, error)
	OnGetDownload   func(ctx context.Context, id string) (*download.Download, error)
	OnGetFiles      func(ctx context.Context, id string) ([]download.File, error)
}

// NewMockDownloader creates a new mock download.
func NewMockDownloader(name string) *MockDownloader {
	return &MockDownloader{
		name:      name,
		dlType:    "mock",
		downloads: make(map[string]*download.Download),
		files:     make(map[string][]download.File),
	}
}

// Name returns the configured name.
func (m *MockDownloader) Name() string {
	return m.name
}

// Type returns the downloader type.
func (m *MockDownloader) Type() string {
	return m.dlType
}

// Connect establishes a connection (no-op for mock).
func (m *MockDownloader) Connect(ctx context.Context) error {
	if m.OnConnect != nil {
		return m.OnConnect(ctx)
	}
	return nil
}

// Close closes the connection (no-op for mock).
func (m *MockDownloader) Close() error {
	return nil
}

// ListDownloads returns downloads matching the given categories.
func (m *MockDownloader) ListDownloads(ctx context.Context, categories []string) ([]download.Download, error) {
	if m.OnListDownloads != nil {
		return m.OnListDownloads(ctx, categories)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	categorySet := make(map[string]bool)
	for _, c := range categories {
		categorySet[c] = true
	}

	var result []download.Download
	for _, dl := range m.downloads {
		// If no categories specified, return all; otherwise filter
		if len(categories) == 0 || categorySet[dl.Category] {
			result = append(result, *dl)
		}
	}
	return result, nil
}

// GetDownload returns a specific download by ID.
func (m *MockDownloader) GetDownload(ctx context.Context, id string) (*download.Download, error) {
	if m.OnGetDownload != nil {
		return m.OnGetDownload(ctx, id)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	dl, ok := m.downloads[id]
	if !ok {
		return nil, ErrNotFound
	}
	return dl, nil
}

// GetFiles returns the files for a download.
func (m *MockDownloader) GetFiles(ctx context.Context, id string) ([]download.File, error) {
	if m.OnGetFiles != nil {
		return m.OnGetFiles(ctx, id)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	files, ok := m.files[id]
	if !ok {
		return nil, ErrNotFound
	}
	return files, nil
}

// OpenFile opens a remote file for reading (not implemented for mock).
func (m *MockDownloader) OpenFile(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

// SSHConfig returns the SSH configuration.
func (m *MockDownloader) SSHConfig() config.SSHConfig {
	return m.sshConfig
}

// AddDownload adds a download to the mock.
func (m *MockDownloader) AddDownload(dl *download.Download, files []download.File) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloads[dl.ID] = dl
	m.files[dl.ID] = files
}

// RemoveDownload removes a download from the mock.
func (m *MockDownloader) RemoveDownload(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.downloads, id)
	delete(m.files, id)
}

// UpdateDownload updates a download in the mock.
func (m *MockDownloader) UpdateDownload(dl *download.Download) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloads[dl.ID] = dl
}

// SetCategory changes a download's category.
func (m *MockDownloader) SetCategory(id, category string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if dl, ok := m.downloads[id]; ok {
		dl.Category = category
	}
}

// MockTransferer is a mock implementation of transfer.Transferer for testing.
type MockTransferer struct {
	mu    sync.RWMutex
	speed int64

	// Track transfer calls
	TransferCalls []transfer.Request

	// Hooks for custom behavior
	OnTransfer func(ctx context.Context, req transfer.Request, onProgress transfer.ProgressFunc) error
}

// NewMockTransferer creates a new mock transferer.
func NewMockTransferer() *MockTransferer {
	return &MockTransferer{}
}

// Transfer copies a file (mock implementation).
func (m *MockTransferer) Transfer(ctx context.Context, req transfer.Request, onProgress transfer.ProgressFunc) error {
	m.mu.Lock()
	m.TransferCalls = append(m.TransferCalls, req)
	m.mu.Unlock()

	if m.OnTransfer != nil {
		return m.OnTransfer(ctx, req, onProgress)
	}

	// Default behavior: create the file and report progress
	if err := m.createFile(req.LocalPath, req.Size); err != nil {
		return err
	}

	// Simulate successful transfer with progress
	if onProgress != nil {
		const bytesPerMB = 1024 * 1024
		onProgress(transfer.Progress{
			Transferred: req.Size,
			BytesPerSec: bytesPerMB, // 1 MB/s
		})
	}
	return nil
}

// createFile creates a file with the given size for testing.
func (m *MockTransferer) createFile(path string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	return os.WriteFile(path, make([]byte, size), 0600)
}

// Name returns the backend name.
func (m *MockTransferer) Name() string {
	return "mock"
}

// GetSpeed returns the current transfer speed.
func (m *MockTransferer) GetSpeed() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.speed
}

// SetSpeed sets the mock speed (for testing).
func (m *MockTransferer) SetSpeed(speed int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speed = speed
}

// PrepareShutdown prepares for shutdown (no-op for mock).
func (m *MockTransferer) PrepareShutdown() {}

// Close releases resources (no-op for mock).
func (m *MockTransferer) Close() error {
	return nil
}

// GetTransferCalls returns the recorded transfer calls.
func (m *MockTransferer) GetTransferCalls() []transfer.Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]transfer.Request, len(m.TransferCalls))
	copy(result, m.TransferCalls)
	return result
}

// MockApp is a mock implementation of app.App for testing.
type MockApp struct {
	name                    string
	appType                 string
	category                string
	downloadsPath           string
	cleanupOnCategoryChange bool
	cleanupOnRemove         bool

	mu           sync.RWMutex
	ImportCalls  []string
	TestConnErr  error
	TriggerError error
}

// NewMockApp creates a new mock app.
func NewMockApp(name, category, downloadsPath string) *MockApp {
	return &MockApp{
		name:          name,
		appType:       "mock",
		category:      category,
		downloadsPath: downloadsPath,
	}
}

// Name returns the app name.
func (m *MockApp) Name() string {
	return m.name
}

// Type returns the app type.
func (m *MockApp) Type() string {
	return m.appType
}

// Category returns the category this app handles.
func (m *MockApp) Category() string {
	return m.category
}

// DownloadsPath returns the downloads path.
func (m *MockApp) DownloadsPath() string {
	return m.downloadsPath
}

// CleanupOnCategoryChange returns whether to cleanup on category change.
func (m *MockApp) CleanupOnCategoryChange() bool {
	return m.cleanupOnCategoryChange
}

// CleanupOnRemove returns whether to cleanup on remove.
func (m *MockApp) CleanupOnRemove() bool {
	return m.cleanupOnRemove
}

// SetCleanupOnCategoryChange sets the cleanup on category change flag.
func (m *MockApp) SetCleanupOnCategoryChange(v bool) {
	m.cleanupOnCategoryChange = v
}

// SetCleanupOnRemove sets the cleanup on remove flag.
func (m *MockApp) SetCleanupOnRemove(v bool) {
	m.cleanupOnRemove = v
}

// TriggerImport triggers an import.
func (m *MockApp) TriggerImport(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.TriggerError != nil {
		return m.TriggerError
	}

	m.ImportCalls = append(m.ImportCalls, path)
	return nil
}

// TestConnection tests the connection.
func (m *MockApp) TestConnection(_ context.Context) error {
	return m.TestConnErr
}

// GetImportCalls returns the recorded import calls.
func (m *MockApp) GetImportCalls() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.ImportCalls))
	copy(result, m.ImportCalls)
	return result
}

// SetTriggerError sets an error to return from TriggerImport.
func (m *MockApp) SetTriggerError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TriggerError = err
}
