package download_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/download"
)

// --- Registry Tests ---

func TestRegistry(t *testing.T) {
	t.Run("NewRegistry", func(t *testing.T) {
		r := download.NewRegistry()
		require.NotNil(t, r)
		assert.Empty(t, r.All())
	})

	t.Run("Register", func(t *testing.T) {
		r := download.NewRegistry()

		// Create a mock downloader using the qBittorrent constructor
		// Since we can't mock easily, we'll test with minimal config
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{
			URL:      server.URL,
			Username: "admin",
			Password: "admin",
		}
		dl := download.NewQBittorrent("seedbox", cfg)

		r.Register("seedbox", dl)

		got, ok := r.Get("seedbox")
		require.True(t, ok)
		assert.Equal(t, dl, got)
	})

	t.Run("RegisterMultipleDownloaders", func(t *testing.T) {
		r := download.NewRegistry()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}

		r.Register("seedbox1", download.NewQBittorrent("seedbox1", cfg))
		r.Register("seedbox2", download.NewQBittorrent("seedbox2", cfg))

		assert.Len(t, r.All(), 2)

		_, ok1 := r.Get("seedbox1")
		_, ok2 := r.Get("seedbox2")
		assert.True(t, ok1)
		assert.True(t, ok2)
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		r := download.NewRegistry()

		got, ok := r.Get("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("All", func(t *testing.T) {
		r := download.NewRegistry()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}
		dl := download.NewQBittorrent("seedbox", cfg)
		r.Register("seedbox", dl)

		all := r.All()
		assert.Len(t, all, 1)
		assert.Equal(t, dl, all["seedbox"])
	})
}

// --- QBittorrent Client Tests ---

func TestQBittorrentClient(t *testing.T) {
	t.Run("NewQBittorrent", func(t *testing.T) {
		cfg := config.DownloaderConfig{
			URL:      "http://localhost:8080",
			Username: "admin",
			Password: "password",
		}

		dl := download.NewQBittorrent("seedbox", cfg)

		assert.Equal(t, "seedbox", dl.Name())
		assert.Equal(t, "qbittorrent", dl.Type())
	})

	t.Run("WithLogger", func(t *testing.T) {
		cfg := config.DownloaderConfig{
			URL: "http://localhost:8080",
		}

		// Should not panic
		dl := download.NewQBittorrent("seedbox", cfg, download.WithLogger(zerolog.Nop()))
		assert.NotNil(t, dl)
	})

	t.Run("SSHConfig", func(t *testing.T) {
		cfg := config.DownloaderConfig{
			URL: "http://localhost:8080",
			SSH: config.SSHConfig{
				Host:    "seedbox.example.com",
				Port:    22,
				User:    "user",
				KeyFile: "/path/to/key",
			},
		}

		dl := download.NewQBittorrent("seedbox", cfg)

		sshCfg := dl.SSHConfig()
		assert.Equal(t, "seedbox.example.com", sshCfg.Host)
		assert.Equal(t, 22, sshCfg.Port)
		assert.Equal(t, "user", sshCfg.User)
		assert.Equal(t, "/path/to/key", sshCfg.KeyFile)
	})

	t.Run("URLNormalization", func(t *testing.T) {
		// Test that trailing slashes are removed from the URL
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Should be /api/v2/app/version, not //api/v2/app/version
			assert.NotContains(t, r.URL.Path, "//")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("4.5.0"))
		}))
		defer server.Close()

		// URL with trailing slash
		cfg := config.DownloaderConfig{
			URL: server.URL + "/",
		}

		dl := download.NewQBittorrent("seedbox", cfg)

		// Login without credentials tests the version endpoint
		// This will fail because SSH isn't configured, but we can verify URL handling
		_ = dl.Connect(t.Context())
	})
}

// --- Login Tests ---

func TestQBittorrentLogin(t *testing.T) {
	t.Run("LoginWithCredentials_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/auth/login":
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

				err := r.ParseForm()
				assert.NoError(t, err)
				assert.Equal(t, "admin", r.FormValue("username"))
				assert.Equal(t, "password", r.FormValue("password"))

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Ok."))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{
			URL:      server.URL,
			Username: "admin",
			Password: "password",
		}

		dl := download.NewQBittorrent("seedbox", cfg)

		err := dl.Connect(t.Context())
		require.NoError(t, err)
	})

	t.Run("LoginWithCredentials_Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/auth/login" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Fails."))
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{
			URL:      server.URL,
			Username: "admin",
			Password: "wrongpassword",
		}

		dl := download.NewQBittorrent("seedbox", cfg)
		err := dl.Connect(t.Context())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "login failed")
	})

	t.Run("LoginWithoutCredentials_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/app/version" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("4.5.0"))
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{
			URL:      server.URL,
			Username: "", // No credentials
			Password: "",
		}

		dl := download.NewQBittorrent("seedbox", cfg)
		err := dl.Connect(t.Context())

		require.NoError(t, err)
	})

	t.Run("LoginWithoutCredentials_Forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/app/version" {
				w.WriteHeader(http.StatusForbidden)
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{
			URL:      server.URL,
			Username: "",
			Password: "",
		}

		dl := download.NewQBittorrent("seedbox", cfg)
		err := dl.Connect(t.Context())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication required")
	})
}

// --- ListDownloads Tests ---

func TestQBittorrentListDownloads_ReturnsAllTorrents(t *testing.T) {
	torrents := []map[string]any{
		{
			"hash":          "abc123",
			"name":          "Show.S01E01",
			"category":      "tv",
			"state":         "uploading",
			"save_path":     "/downloads/tv",
			"content_path":  "/downloads/tv/Show.S01E01",
			"size":          int64(1000000000),
			"downloaded":    int64(1000000000),
			"progress":      1.0,
			"added_on":      time.Now().Unix(),
			"completion_on": time.Now().Unix(),
		},
		{
			"hash":       "def456",
			"name":       "Movie.2024",
			"category":   "movies",
			"state":      "downloading",
			"save_path":  "/downloads/movies",
			"size":       int64(5000000000),
			"downloaded": int64(2500000000),
			"progress":   0.5,
			"added_on":   time.Now().Unix(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/torrents/info" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(torrents)
		}
	}))
	defer server.Close()

	cfg := config.DownloaderConfig{URL: server.URL}
	dl := download.NewQBittorrent("seedbox", cfg)

	downloads, err := dl.ListDownloads(t.Context(), nil)
	require.NoError(t, err)

	require.Len(t, downloads, 2)

	// First torrent (completed)
	assert.Equal(t, "abc123", downloads[0].ID)
	assert.Equal(t, "Show.S01E01", downloads[0].Name)
	assert.Equal(t, "tv", downloads[0].Category)
	assert.Equal(t, download.TorrentStateComplete, downloads[0].State)
	assert.InDelta(t, 1.0, downloads[0].Progress, 0.01)

	// Second torrent (downloading)
	assert.Equal(t, "def456", downloads[1].ID)
	assert.Equal(t, download.TorrentStateDownloading, downloads[1].State)
}

func TestQBittorrentListDownloads_FilterByCategory(t *testing.T) {
	torrents := []map[string]any{
		{"hash": "abc123", "name": "Show", "category": "tv", "state": "uploading", "progress": 1.0},
		{"hash": "def456", "name": "Movie", "category": "movies", "state": "uploading", "progress": 1.0},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/torrents/info" {
			category := r.URL.Query().Get("category")
			if category != "" {
				// Filter by category
				var filtered []map[string]any
				for _, torrent := range torrents {
					if torrent["category"] == category {
						filtered = append(filtered, torrent)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(filtered)
			} else {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(torrents)
			}
		}
	}))
	defer server.Close()

	cfg := config.DownloaderConfig{URL: server.URL}
	dl := download.NewQBittorrent("seedbox", cfg)

	// Filter by single category
	downloads, err := dl.ListDownloads(t.Context(), []string{"tv"})
	require.NoError(t, err)

	require.Len(t, downloads, 1)
	assert.Equal(t, "tv", downloads[0].Category)
}

func TestQBittorrentListDownloads_FilterByMultipleCategories(t *testing.T) {
	torrents := []map[string]any{
		{"hash": "abc123", "name": "Show", "category": "tv", "state": "uploading", "progress": 1.0},
		{"hash": "def456", "name": "Movie", "category": "movies", "state": "uploading", "progress": 1.0},
		{"hash": "ghi789", "name": "Other", "category": "misc", "state": "uploading", "progress": 1.0},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(torrents)
	}))
	defer server.Close()

	cfg := config.DownloaderConfig{URL: server.URL}
	dl := download.NewQBittorrent("seedbox", cfg)

	// Filter by multiple categories (client-side filtering)
	downloads, err := dl.ListDownloads(t.Context(), []string{"tv", "movies"})
	require.NoError(t, err)

	require.Len(t, downloads, 2)
	categories := []string{downloads[0].Category, downloads[1].Category}
	assert.Contains(t, categories, "tv")
	assert.Contains(t, categories, "movies")
	assert.NotContains(t, categories, "misc")
}

func TestQBittorrentListDownloads_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	cfg := config.DownloaderConfig{URL: server.URL}
	dl := download.NewQBittorrent("seedbox", cfg)

	downloads, err := dl.ListDownloads(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, downloads)
}

// --- GetDownload Tests ---

func TestQBittorrentGetDownload(t *testing.T) {
	t.Run("GetDownload_Found", func(t *testing.T) {
		torrent := []map[string]any{
			{
				"hash":         "abc123",
				"name":         "Show.S01E01",
				"category":     "tv",
				"state":        "uploading",
				"save_path":    "/downloads/tv",
				"content_path": "/downloads/tv/Show.S01E01",
				"size":         int64(1000000000),
				"downloaded":   int64(1000000000),
				"progress":     1.0,
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/torrents/info" {
				assert.Equal(t, "abc123", r.URL.Query().Get("hashes"))
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(torrent)
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}
		dl := download.NewQBittorrent("seedbox", cfg)

		d, err := dl.GetDownload(t.Context(), "abc123")
		require.NoError(t, err)
		require.NotNil(t, d)

		assert.Equal(t, "abc123", d.ID)
		assert.Equal(t, "Show.S01E01", d.Name)
		assert.Equal(t, download.TorrentStateComplete, d.State)
	})

	t.Run("GetDownload_NotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}
		dl := download.NewQBittorrent("seedbox", cfg)

		d, err := dl.GetDownload(t.Context(), "nonexistent")
		require.Error(t, err)
		assert.Nil(t, d)
		assert.Contains(t, err.Error(), "not found")
	})
}

// --- GetFiles Tests ---

func TestQBittorrentGetFiles(t *testing.T) {
	t.Run("GetFiles_ReturnsFileList", func(t *testing.T) {
		files := []map[string]any{
			{
				"index":    0,
				"name":     "Show.S01E01/episode.mkv",
				"size":     int64(1000000000),
				"progress": 1.0,
				"priority": 1,
			},
			{
				"index":    1,
				"name":     "Show.S01E01/subs.srt",
				"size":     int64(50000),
				"progress": 0.5,
				"priority": 1,
			},
			{
				"index":    2,
				"name":     "Show.S01E01/sample.mkv",
				"size":     int64(10000000),
				"progress": 1.0,
				"priority": 0, // Not selected for download
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/torrents/files" {
				assert.Equal(t, "abc123", r.URL.Query().Get("hash"))
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(files)
			}
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}
		dl := download.NewQBittorrent("seedbox", cfg)

		result, err := dl.GetFiles(t.Context(), "abc123")
		require.NoError(t, err)

		require.Len(t, result, 3)

		// First file (complete)
		assert.Equal(t, "Show.S01E01/episode.mkv", result[0].Path)
		assert.Equal(t, int64(1000000000), result[0].Size)
		assert.Equal(t, download.FileStateComplete, result[0].State)
		assert.Equal(t, 1, result[0].Priority)

		// Second file (downloading)
		assert.Equal(t, download.FileStateDownloading, result[1].State)
		assert.Equal(t, int64(25000), result[1].Downloaded) // 50% of 50000

		// Third file (skipped)
		assert.Equal(t, 0, result[2].Priority)
	})

	t.Run("GetFiles_Empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		defer server.Close()

		cfg := config.DownloaderConfig{URL: server.URL}
		dl := download.NewQBittorrent("seedbox", cfg)

		result, err := dl.GetFiles(t.Context(), "abc123")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

// --- State Mapping Tests ---

func TestTorrentStateMapping(t *testing.T) {
	tests := []struct {
		name          string
		qbtState      string
		progress      float64
		expectedState download.TorrentState
	}{
		// Complete states
		{"uploading", "uploading", 1.0, download.TorrentStateComplete},
		{"stalledUP", "stalledUP", 1.0, download.TorrentStateComplete},
		{"queuedUP", "queuedUP", 1.0, download.TorrentStateComplete},
		{"forcedUP", "forcedUP", 1.0, download.TorrentStateComplete},
		{"checkingUP", "checkingUP", 1.0, download.TorrentStateComplete},

		// Paused states (qBittorrent v4.4 and earlier)
		{"pausedDL", "pausedDL", 0.5, download.TorrentStatePaused},
		// pausedUP with progress=1.0 is Complete because progress override
		{"pausedUP", "pausedUP", 1.0, download.TorrentStateComplete},

		// Stopped states (qBittorrent v4.5+)
		{"stoppedDL", "stoppedDL", 0.5, download.TorrentStatePaused},
		// stoppedUP with progress=1.0 is Complete because progress override
		{"stoppedUP", "stoppedUP", 1.0, download.TorrentStateComplete},

		// Error states
		{"error", "error", 0.5, download.TorrentStateError},
		{"missingFiles", "missingFiles", 0.5, download.TorrentStateError},

		// Downloading states
		{"downloading", "downloading", 0.5, download.TorrentStateDownloading},
		{"stalledDL", "stalledDL", 0.5, download.TorrentStateDownloading},
		{"metaDL", "metaDL", 0.0, download.TorrentStateDownloading},
		{"forcedDL", "forcedDL", 0.5, download.TorrentStateDownloading},
		{"checkingDL", "checkingDL", 0.5, download.TorrentStateDownloading},

		// Progress override (progress=1.0 means complete regardless of state)
		{"downloading_but_complete", "downloading", 1.0, download.TorrentStateComplete},
		{"stalledDL_but_complete", "stalledDL", 1.0, download.TorrentStateComplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := []map[string]any{
				{
					"hash":     "abc123",
					"name":     "Test",
					"category": "test",
					"state":    tt.qbtState,
					"progress": tt.progress,
				},
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(torrent)
			}))
			defer server.Close()

			cfg := config.DownloaderConfig{URL: server.URL}
			dl := download.NewQBittorrent("seedbox", cfg)

			d, err := dl.GetDownload(t.Context(), "abc123")
			require.NoError(t, err)

			assert.Equal(t, tt.expectedState, d.State, "state mismatch for qBittorrent state %s", tt.qbtState)
		})
	}
}

// --- Timestamp Tests ---

func TestTimestampHandling(t *testing.T) {
	now := time.Now()
	completedTime := now.Add(-time.Hour)

	torrent := []map[string]any{
		{
			"hash":          "abc123",
			"name":          "Test",
			"category":      "test",
			"state":         "uploading",
			"progress":      1.0,
			"added_on":      now.Unix(),
			"completion_on": completedTime.Unix(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(torrent)
	}))
	defer server.Close()

	cfg := config.DownloaderConfig{URL: server.URL}
	dl := download.NewQBittorrent("seedbox", cfg)

	d, err := dl.GetDownload(t.Context(), "abc123")
	require.NoError(t, err)

	// Timestamps should be converted from Unix epoch
	assert.WithinDuration(t, now, d.AddedOn, time.Second)
	assert.WithinDuration(t, completedTime, d.CompletedOn, time.Second)
}

// --- Close Tests ---

func TestQBittorrentClose(t *testing.T) {
	t.Run("CloseWithoutConnection", func(t *testing.T) {
		cfg := config.DownloaderConfig{URL: "http://localhost:8080"}
		dl := download.NewQBittorrent("seedbox", cfg)

		// Should not panic or error when closing without connecting
		err := dl.Close()
		assert.NoError(t, err)
	})
}
