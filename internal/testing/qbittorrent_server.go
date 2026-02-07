package testing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// FakeTorrent represents a torrent in the mock qBittorrent server.
type FakeTorrent struct {
	Hash        string
	Name        string
	Category    string
	State       string  // "downloading", "uploading", "pausedDL", "stalledUP", etc.
	Progress    float64 // 0.0 to 1.0
	Size        int64
	Downloaded  int64
	SavePath    string
	ContentPath string
	AddedOn     int64 // Unix timestamp
	CompletedOn int64 // Unix timestamp (0 if not complete)
}

// FakeFile represents a file in a torrent.
type FakeFile struct {
	Index    int
	Name     string // Relative path within torrent
	Size     int64
	Progress float64 // 0.0 to 1.0
	Priority int     // 0=skip, 1-7=normal priorities
}

// QBittorrentServer is a mock qBittorrent API server for testing.
type QBittorrentServer struct {
	*httptest.Server

	mu       sync.RWMutex
	torrents map[string]*FakeTorrent
	files    map[string][]FakeFile
}

// NewQBittorrentServer creates a new mock qBittorrent server.
func NewQBittorrentServer() *QBittorrentServer {
	s := &QBittorrentServer{
		torrents: make(map[string]*FakeTorrent),
		files:    make(map[string][]FakeFile),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v2/app/version", s.handleVersion)
	mux.HandleFunc("GET /api/v2/torrents/info", s.handleTorrentsInfo)
	mux.HandleFunc("GET /api/v2/torrents/files", s.handleTorrentsFiles)

	s.Server = httptest.NewServer(mux)
	return s
}

// AddTorrent adds a torrent to the mock server.
func (s *QBittorrentServer) AddTorrent(t *FakeTorrent, files []FakeFile) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.torrents[t.Hash] = t
	s.files[t.Hash] = files
}

// GetTorrent returns a torrent by hash.
func (s *QBittorrentServer) GetTorrent(hash string) *FakeTorrent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.torrents[hash]
}

// SetTorrentState updates a torrent's state and progress.
// Also updates all files' progress to match the torrent progress.
func (s *QBittorrentServer) SetTorrentState(hash, state string, progress float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t, ok := s.torrents[hash]; ok {
		t.State = state
		t.Progress = progress
		if progress >= 1.0 {
			t.Downloaded = t.Size
		} else {
			t.Downloaded = int64(float64(t.Size) * progress)
		}
	}

	// Also update file progress
	if files, ok := s.files[hash]; ok {
		for i := range files {
			files[i].Progress = progress
		}
	}
}

// SetTorrentCategory changes a torrent's category.
func (s *QBittorrentServer) SetTorrentCategory(hash, category string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t, ok := s.torrents[hash]; ok {
		t.Category = category
	}
}

// RemoveTorrent removes a torrent from the mock server.
func (s *QBittorrentServer) RemoveTorrent(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.torrents, hash)
	delete(s.files, hash)
}

// Reset clears all torrents from the server.
func (s *QBittorrentServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.torrents = make(map[string]*FakeTorrent)
	s.files = make(map[string][]FakeFile)
}

// handleLogin handles POST /api/v2/auth/login.
func (s *QBittorrentServer) handleLogin(w http.ResponseWriter, _ *http.Request) {
	// Always succeed - we don't care about auth in tests
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ok."))
}

// handleVersion handles GET /api/v2/app/version.
func (s *QBittorrentServer) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("v4.6.0"))
}

// qbAPITorrent matches the qBittorrent API response format.
type qbAPITorrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	State       string  `json:"state"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
	Size        int64   `json:"size"`
	Downloaded  int64   `json:"downloaded"`
	Progress    float64 `json:"progress"`
	AddedOn     int64   `json:"added_on"`
	CompletedOn int64   `json:"completion_on"`
}

// handleTorrentsInfo handles GET /api/v2/torrents/info.
func (s *QBittorrentServer) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Parse query parameters
	category := r.URL.Query().Get("category")
	hashes := r.URL.Query().Get("hashes")

	var hashFilter map[string]bool
	if hashes != "" {
		hashFilter = make(map[string]bool)
		for h := range strings.SplitSeq(hashes, "|") {
			hashFilter[h] = true
		}
	}

	var result []qbAPITorrent
	for _, t := range s.torrents {
		// Filter by category if specified
		if category != "" && t.Category != category {
			continue
		}
		// Filter by hash if specified
		if hashFilter != nil && !hashFilter[t.Hash] {
			continue
		}

		result = append(result, qbAPITorrent{
			Hash:        t.Hash,
			Name:        t.Name,
			Category:    t.Category,
			State:       t.State,
			SavePath:    t.SavePath,
			ContentPath: t.ContentPath,
			Size:        t.Size,
			Downloaded:  t.Downloaded,
			Progress:    t.Progress,
			AddedOn:     t.AddedOn,
			CompletedOn: t.CompletedOn,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// qbAPIFile matches the qBittorrent API response format for files.
type qbAPIFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// handleTorrentsFiles handles GET /api/v2/torrents/files.
func (s *QBittorrentServer) handleTorrentsFiles(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "hash required", http.StatusBadRequest)
		return
	}

	files, ok := s.files[hash]
	if !ok {
		// Return empty array for unknown hash (qBittorrent behavior)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}

	result := make([]qbAPIFile, len(files))
	for i, f := range files {
		result[i] = qbAPIFile(f)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
