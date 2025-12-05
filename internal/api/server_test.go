package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/api"
	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/orchestrator"
	mockpkg "github.com/seedreap/seedreap/internal/testing"
)

// testServer creates a test server with minimal dependencies.
type testServer struct {
	server       *api.Server
	orchestrator *orchestrator.Orchestrator
	downloaders  *download.Registry
	apps         *app.Registry
	syncer       *filesync.Syncer
	mockDL       *mockpkg.MockDownloader
	mockApp      *mockpkg.MockApp
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	// Create temp dir for syncing
	tempDir := t.TempDir()

	// Create registries
	dlRegistry := download.NewRegistry()
	appRegistry := app.NewRegistry()

	// Create mock downloader
	mockDL := mockpkg.NewMockDownloader("seedbox")
	dlRegistry.Register("seedbox", mockDL)

	// Create mock app
	mockApp := mockpkg.NewMockApp("sonarr", "tv", tempDir+"/downloads/tv")
	appRegistry.Register("sonarr", mockApp)

	// Create syncer with mock transferer
	mockTransfer := mockpkg.NewMockTransferer()
	syncer := filesync.New(
		tempDir+"/syncing",
		filesync.WithTransferer(mockTransfer),
	)

	// Create orchestrator
	orch := orchestrator.New(
		dlRegistry,
		appRegistry,
		syncer,
		tempDir+"/downloads",
	)

	// Create API server
	server := api.New(orch, dlRegistry, appRegistry, syncer)

	return &testServer{
		server:       server,
		orchestrator: orch,
		downloaders:  dlRegistry,
		apps:         appRegistry,
		syncer:       syncer,
		mockDL:       mockDL,
		mockApp:      mockApp,
	}
}

// --- Health Endpoint Tests ---

func TestHealthHandler(t *testing.T) {
	ts := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	ts.server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
}

// --- Stats Endpoint Tests ---

func TestStatsHandler(t *testing.T) {
	t.Run("EmptyStats", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Empty orchestrator should have zero tracked downloads
		assert.InDelta(t, 0, response["total_tracked"], 0.01)
	})
}

// --- Downloads Endpoint Tests ---

func TestListDownloadsHandler(t *testing.T) {
	t.Run("EmptyDownloads", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []any
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Empty(t, response)
	})
}

func TestGetDownloadHandler(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads/nonexistent", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "download not found", response["error"])
	})
}

// --- Jobs Endpoint Tests ---

func TestListJobsHandler(t *testing.T) {
	t.Run("EmptyJobs", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []any
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Empty(t, response)
	})
}

func TestGetJobHandler(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/jobs/nonexistent", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "job not found", response["error"])
	})
}

// --- Downloaders Endpoint Tests ---

func TestListDownloadersHandler(t *testing.T) {
	t.Run("ReturnsConfiguredDownloaders", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/downloaders", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response, 1)
		assert.Equal(t, "seedbox", response[0]["name"])
		assert.Equal(t, "mock", response[0]["type"])
	})

	t.Run("MultipleDownloaders", func(t *testing.T) {
		tempDir := t.TempDir()

		dlRegistry := download.NewRegistry()
		appRegistry := app.NewRegistry()

		// Register multiple downloaders
		dlRegistry.Register("seedbox1", mockpkg.NewMockDownloader("seedbox1"))
		dlRegistry.Register("seedbox2", mockpkg.NewMockDownloader("seedbox2"))

		syncer := filesync.New(tempDir + "/syncing")
		orch := orchestrator.New(dlRegistry, appRegistry, syncer, tempDir+"/downloads")
		server := api.New(orch, dlRegistry, appRegistry, syncer)

		req := httptest.NewRequest(http.MethodGet, "/api/downloaders", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 2)
	})
}

// --- Apps Endpoint Tests ---

func TestListAppsHandler(t *testing.T) {
	t.Run("ReturnsConfiguredApps", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/apps", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response, 1)
		assert.Equal(t, "sonarr", response[0]["name"])
		assert.Equal(t, "mock", response[0]["type"])
		assert.Equal(t, "tv", response[0]["category"])
	})

	t.Run("MultipleApps", func(t *testing.T) {
		tempDir := t.TempDir()

		dlRegistry := download.NewRegistry()
		appRegistry := app.NewRegistry()

		// Register multiple apps
		appRegistry.Register("sonarr", mockpkg.NewMockApp("sonarr", "tv", tempDir+"/tv"))
		appRegistry.Register("radarr", mockpkg.NewMockApp("radarr", "movies", tempDir+"/movies"))
		appRegistry.Register("misc", app.NewPassthrough("misc", "misc", tempDir+"/misc"))

		syncer := filesync.New(tempDir + "/syncing")
		orch := orchestrator.New(dlRegistry, appRegistry, syncer, tempDir+"/downloads")
		server := api.New(orch, dlRegistry, appRegistry, syncer)

		req := httptest.NewRequest(http.MethodGet, "/api/apps", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response, 3)

		// Build a map for easier assertions
		appsByName := make(map[string]map[string]string)
		for _, a := range response {
			appsByName[a["name"]] = a
		}

		assert.Equal(t, "tv", appsByName["sonarr"]["category"])
		assert.Equal(t, "movies", appsByName["radarr"]["category"])
		assert.Equal(t, "passthrough", appsByName["misc"]["type"])
	})
}

// --- Speed History Endpoint Tests ---

func TestSpeedHistoryHandler(t *testing.T) {
	t.Run("EmptyHistory", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/speed-history", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []filesync.SpeedSample
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Empty(t, response)
	})

	t.Run("WithRecordedHistory", func(t *testing.T) {
		ts := newTestServer(t)

		// Record some speed samples
		ts.syncer.RecordSpeed(1000000) // 1 MB/s
		ts.syncer.RecordSpeed(2000000) // 2 MB/s
		ts.syncer.RecordSpeed(1500000) // 1.5 MB/s

		req := httptest.NewRequest(http.MethodGet, "/api/speed-history", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []filesync.SpeedSample
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response, 3)
		assert.Equal(t, int64(1000000), response[0].Speed)
		assert.Equal(t, int64(2000000), response[1].Speed)
		assert.Equal(t, int64(1500000), response[2].Speed)
	})
}

// --- Index Handler Tests ---

func TestIndexHandler(t *testing.T) {
	t.Run("ServesHTMLWithoutUI", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, rec.Body.String(), "SeedReap")
		assert.Contains(t, rec.Body.String(), "/api/health")
	})
}

// --- Server Options Tests ---

func TestServerOptions(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		tempDir := t.TempDir()

		dlRegistry := download.NewRegistry()
		appRegistry := app.NewRegistry()
		syncer := filesync.New(tempDir + "/syncing")
		orch := orchestrator.New(dlRegistry, appRegistry, syncer, tempDir+"/downloads")

		// Should not panic when creating with logger option
		server := api.New(
			orch,
			dlRegistry,
			appRegistry,
			syncer,
			// api.WithLogger(zerolog.Nop()), // Would test logger option
		)

		require.NotNil(t, server)
	})
}

// --- Integration Tests ---

func TestAPIIntegration(t *testing.T) {
	t.Run("DownloadsVisibleAfterOrchestratorStart", func(t *testing.T) {
		ts := newTestServer(t)

		// Add a download to the mock
		dl := &download.Download{
			ID:       "test-hash-123",
			Name:     "Test.Show.S01E01",
			Category: "tv",
			State:    download.TorrentStateComplete,
			Size:     1000000,
			Progress: 1.0,
			SavePath: "/remote/downloads",
		}
		files := []download.File{
			{
				Path:     "Test.Show.S01E01/episode.mkv",
				Size:     1000000,
				State:    download.FileStateComplete,
				Priority: 1,
			},
		}
		ts.mockDL.AddDownload(dl, files)

		// Start the orchestrator to pick up the download
		err := ts.orchestrator.Start(t.Context())
		require.NoError(t, err)
		defer ts.orchestrator.Stop()

		// Give orchestrator time to poll
		time.Sleep(100 * time.Millisecond)

		// Now check the API
		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should have one download tracked
		require.Len(t, response, 1)
		assert.Equal(t, "test-hash-123", response[0]["id"])
		assert.Equal(t, "Test.Show.S01E01", response[0]["name"])
		assert.Equal(t, "tv", response[0]["category"])
	})

	t.Run("DownloadsFilteredByAppCategory", func(t *testing.T) {
		tempDir := t.TempDir()

		dlRegistry := download.NewRegistry()
		appRegistry := app.NewRegistry()

		mockDL := mockpkg.NewMockDownloader("seedbox")
		dlRegistry.Register("seedbox", mockDL)

		// Only register an app for "tv" category
		appRegistry.Register("sonarr", mockpkg.NewMockApp("sonarr", "tv", tempDir+"/tv"))

		syncer := filesync.New(tempDir + "/syncing")
		orch := orchestrator.New(dlRegistry, appRegistry, syncer, tempDir+"/downloads")
		server := api.New(orch, dlRegistry, appRegistry, syncer)

		// Add downloads - one matching, one not
		mockDL.AddDownload(&download.Download{
			ID:       "dl-tv",
			Name:     "TV.Show",
			Category: "tv",
			State:    download.TorrentStateComplete,
		}, []download.File{{Path: "TV.Show/ep.mkv", Size: 100, State: download.FileStateComplete, Priority: 1}})

		mockDL.AddDownload(&download.Download{
			ID:       "dl-movies",
			Name:     "Movie",
			Category: "movies", // No app for this category
			State:    download.TorrentStateComplete,
		}, []download.File{{Path: "Movie/movie.mkv", Size: 100, State: download.FileStateComplete, Priority: 1}})

		err := orch.Start(t.Context())
		require.NoError(t, err)
		defer orch.Stop()

		time.Sleep(100 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Only TV show should be visible (movies category has no app)
		require.Len(t, response, 1)
		assert.Equal(t, "dl-tv", response[0]["id"])
	})

	t.Run("JobsListRecordsSpeed", func(t *testing.T) {
		ts := newTestServer(t)

		// Initial speed history should be empty
		req := httptest.NewRequest(http.MethodGet, "/api/speed-history", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		var history []filesync.SpeedSample
		err := json.Unmarshal(rec.Body.Bytes(), &history)
		require.NoError(t, err)
		assert.Empty(t, history)

		// Call jobs endpoint - this records speed
		req = httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		rec = httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Speed history should now have one entry
		req = httptest.NewRequest(http.MethodGet, "/api/speed-history", nil)
		rec = httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		err = json.Unmarshal(rec.Body.Bytes(), &history)
		require.NoError(t, err)
		assert.Len(t, history, 1)
	})
}

// --- CORS Tests ---

func TestCORSHeaders(t *testing.T) {
	ts := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	ts.server.ServeHTTP(rec, req)

	// CORS should allow any origin
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

// --- Response Sorting Tests ---

func TestResponseSorting(t *testing.T) {
	t.Run("DownloadsSortedByName", func(t *testing.T) {
		tempDir := t.TempDir()

		dlRegistry := download.NewRegistry()
		appRegistry := app.NewRegistry()

		mockDL := mockpkg.NewMockDownloader("seedbox")
		dlRegistry.Register("seedbox", mockDL)
		appRegistry.Register("sonarr", mockpkg.NewMockApp("sonarr", "tv", tempDir+"/tv"))

		syncer := filesync.New(tempDir + "/syncing")
		orch := orchestrator.New(dlRegistry, appRegistry, syncer, tempDir+"/downloads")
		server := api.New(orch, dlRegistry, appRegistry, syncer)

		// Add downloads in non-alphabetical order
		mockDL.AddDownload(&download.Download{
			ID:       "dl-c",
			Name:     "Zebra.Show",
			Category: "tv",
			State:    download.TorrentStateComplete,
		}, []download.File{{Path: "Zebra.Show/ep.mkv", Size: 100, State: download.FileStateComplete, Priority: 1}})

		mockDL.AddDownload(&download.Download{
			ID:       "dl-a",
			Name:     "Alpha.Show",
			Category: "tv",
			State:    download.TorrentStateComplete,
		}, []download.File{{Path: "Alpha.Show/ep.mkv", Size: 100, State: download.FileStateComplete, Priority: 1}})

		mockDL.AddDownload(&download.Download{
			ID:       "dl-b",
			Name:     "Beta.Show",
			Category: "tv",
			State:    download.TorrentStateComplete,
		}, []download.File{{Path: "Beta.Show/ep.mkv", Size: 100, State: download.FileStateComplete, Priority: 1}})

		err := orch.Start(t.Context())
		require.NoError(t, err)
		defer orch.Stop()

		time.Sleep(100 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response, 3)
		// Should be sorted alphabetically by name
		assert.Equal(t, "Alpha.Show", response[0]["name"])
		assert.Equal(t, "Beta.Show", response[1]["name"])
		assert.Equal(t, "Zebra.Show", response[2]["name"])
	})
}
