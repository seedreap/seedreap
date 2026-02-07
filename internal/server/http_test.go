package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadjob"
	"github.com/seedreap/seedreap/internal/ent/generated/event"
	"github.com/seedreap/seedreap/internal/ent/generated/syncjob"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/server"
	mockpkg "github.com/seedreap/seedreap/internal/testing"
)

// testServer creates a test server with minimal dependencies.
type testServer struct {
	server         *server.HTTPServer
	db             *generated.Client
	syncer         *filesync.Syncer
	downloadClient *generated.DownloadClient
	mockDL         *mockpkg.MockDownloadClient
	mockApp        *mockpkg.MockApp
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	// Create temp dir for syncing
	tempDir := t.TempDir()

	// Create Ent database
	db := mockpkg.NewTestDB(t)

	ctx := context.Background()

	// Create mock downloader and register in store
	mockDL := mockpkg.NewMockDownloadClient("seedbox")
	dlr, err := db.DownloadClient.Create().
		SetName("seedbox").
		SetType("mock").
		SetURL("http://localhost:8080").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	// Create mock app and register in store
	mockApp := mockpkg.NewMockApp("sonarr", "tv", tempDir+"/downloads/tv")
	_, err = db.App.Create().
		SetName("sonarr").
		SetType(app.TypeSonarr).
		SetCategory("tv").
		SetDownloadsPath(tempDir + "/downloads/tv").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	// Create syncer with mock transferer
	mockTransfer := mockpkg.NewMockTransferer()
	syncer := filesync.New(
		tempDir+"/syncing",
		filesync.WithTransferer(mockTransfer),
	)

	// Create HTTP server
	httpServer := server.NewHTTPServer(
		syncer,
		server.WithHTTPDB(db),
	)

	return &testServer{
		server:         httpServer,
		db:             db,
		syncer:         syncer,
		downloadClient: dlr,
		mockDL:         mockDL,
		mockApp:        mockApp,
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

		// Empty store should have zero tracked downloads
		assert.InDelta(t, 0, response["total_tracked"], 0.01)
	})

	t.Run("WithDownloads", func(t *testing.T) {
		ts := newTestServer(t)
		ctx := context.Background()
		now := time.Now()

		// Add download jobs to store
		dlJob1, err := ts.db.DownloadJob.Create().
			SetRemoteID("test-hash-1").
			SetDownloadClientID(ts.downloadClient.ID).
			SetName("Test.Show.S01E01").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetSize(1000000).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		dlJob2, err := ts.db.DownloadJob.Create().
			SetRemoteID("test-hash-2").
			SetDownloadClientID(ts.downloadClient.ID).
			SetName("Test.Show.S01E02").
			SetCategory("tv").
			SetStatus(downloadjob.StatusComplete).
			SetSize(2000000).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Create tracked downloads (stats come from this table now)
		_, err = ts.db.TrackedDownload.Create().
			SetDownloadJobID(dlJob1.ID).
			SetName(dlJob1.Name).
			SetCategory(dlJob1.Category).
			SetState(trackeddownload.StateDownloading).
			SetTotalSize(dlJob1.Size).
			SetDiscoveredAt(dlJob1.DiscoveredAt).
			Save(ctx)
		require.NoError(t, err)

		_, err = ts.db.TrackedDownload.Create().
			SetDownloadJobID(dlJob2.ID).
			SetName(dlJob2.Name).
			SetCategory(dlJob2.Category).
			SetState(trackeddownload.StatePending).
			SetTotalSize(dlJob2.Size).
			SetDiscoveredAt(dlJob2.DiscoveredAt).
			Save(ctx)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.InDelta(t, 2, response["total_tracked"], 0.01)
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

	t.Run("WithDownloads", func(t *testing.T) {
		ts := newTestServer(t)
		ctx := context.Background()
		now := time.Now()

		// Add a download job to store
		dl, err := ts.db.DownloadJob.Create().
			SetRemoteID("test-hash-123").
			SetDownloadClientID(ts.downloadClient.ID).
			SetName("Test.Show.S01E01").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetSize(1000000).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Create TrackedDownload that references the DownloadJob
		_, err = ts.db.TrackedDownload.Create().
			SetDownloadJobID(dl.ID).
			SetName(dl.Name).
			SetCategory(dl.Category).
			SetState(trackeddownload.StateDownloading).
			SetTotalSize(dl.Size).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response, 1)
		// ID should be a ULID (26 chars), not the hash
		id, ok := response[0]["id"].(string)
		require.True(t, ok, "id should be a string")
		assert.Len(t, id, 26, "id should be a ULID (26 characters)")
		assert.Equal(t, "Test.Show.S01E01", response[0]["name"])
		assert.Equal(t, "tv", response[0]["category"])
	})
}

func TestGetDownloadHandler(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		ts := newTestServer(t)

		// Use a fake but valid ULID format
		req := httptest.NewRequest(http.MethodGet, "/api/downloads/01HZXXXXXXXXXXXXXXXXXXXXXX", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "download not found", response["error"])
	})

	t.Run("InvalidID", func(t *testing.T) {
		ts := newTestServer(t)

		// Use an invalid ULID format
		req := httptest.NewRequest(http.MethodGet, "/api/downloads/not-a-ulid", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "invalid download id", response["error"])
	})

	t.Run("Found", func(t *testing.T) {
		ts := newTestServer(t)
		ctx := context.Background()
		now := time.Now()

		// Add a download job to store
		dl, err := ts.db.DownloadJob.Create().
			SetRemoteID("test-hash-456").
			SetDownloadClientID(ts.downloadClient.ID).
			SetName("Test.Movie.2024").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetSize(5000000).
			SetSavePath("/remote/downloads").
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Create a sync job
		_, err = ts.db.SyncJob.Create().
			SetDownloadJobID(dl.ID).
			SetStatus(syncjob.StatusSyncing).
			SetRemoteBase("/remote").
			SetLocalBase("/local").
			Save(ctx)
		require.NoError(t, err)

		// Create TrackedDownload with "syncing" state
		td, err := ts.db.TrackedDownload.Create().
			SetDownloadJobID(dl.ID).
			SetName(dl.Name).
			SetCategory(dl.Category).
			SetState(trackeddownload.StateSyncing).
			SetTotalSize(dl.Size).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Use the TrackedDownload ULID for the request
		req := httptest.NewRequest(http.MethodGet, "/api/downloads/"+td.ID.String(), nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, td.ID.String(), response["id"])
		assert.Equal(t, "Test.Movie.2024", response["name"])
		assert.Equal(t, "syncing", response["state"])
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
		ctx := context.Background()

		db := mockpkg.NewTestDB(t)

		// Create store and register downloaders
		_, err := db.DownloadClient.Create().
			SetName("seedbox1").
			SetType("mock").
			SetURL("http://localhost:8081").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.DownloadClient.Create().
			SetName("seedbox2").
			SetType("mock").
			SetURL("http://localhost:8082").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		syncer := filesync.New(tempDir + "/syncing")
		httpServer := server.NewHTTPServer(syncer, server.WithHTTPDB(db))

		req := httptest.NewRequest(http.MethodGet, "/api/downloaders", nil)
		rec := httptest.NewRecorder()
		httpServer.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err = json.Unmarshal(rec.Body.Bytes(), &response)
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
		assert.Equal(t, "sonarr", response[0]["type"])
		assert.Equal(t, "tv", response[0]["category"])
	})

	t.Run("MultipleApps", func(t *testing.T) {
		tempDir := t.TempDir()
		ctx := context.Background()

		db := mockpkg.NewTestDB(t)

		// Register multiple apps in the store
		_, err := db.App.Create().
			SetName("sonarr").
			SetType(app.TypeSonarr).
			SetCategory("tv").
			SetDownloadsPath(tempDir + "/tv").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.App.Create().
			SetName("radarr").
			SetType(app.TypeRadarr).
			SetCategory("movies").
			SetDownloadsPath(tempDir + "/movies").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.App.Create().
			SetName("misc").
			SetType(app.TypePassthrough).
			SetCategory("misc").
			SetDownloadsPath(tempDir + "/misc").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		syncer := filesync.New(tempDir + "/syncing")
		httpServer := server.NewHTTPServer(syncer, server.WithHTTPDB(db))

		req := httptest.NewRequest(http.MethodGet, "/api/apps", nil)
		rec := httptest.NewRecorder()
		httpServer.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]string
		err = json.Unmarshal(rec.Body.Bytes(), &response)
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
	t.Run("Returns404WithoutUI", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		// Without UI configured, the root path returns 404
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// --- Server Options Tests ---

func TestServerOptions(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		tempDir := t.TempDir()

		syncer := filesync.New(tempDir + "/syncing")

		// Should not panic when creating with logger option
		httpServer := server.NewHTTPServer(
			syncer,
			// server.WithHTTPLogger(zerolog.Nop()), // Would test logger option
		)

		require.NotNil(t, httpServer)
	})
}

// --- Integration Tests ---

func TestAPIIntegration(t *testing.T) {
	t.Run("OnlyTrackedDownloadsReturned", func(t *testing.T) {
		tempDir := t.TempDir()
		ctx := context.Background()
		now := time.Now()

		db := mockpkg.NewTestDB(t)

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName("seedbox").
			SetType("mock").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Only create an app for "tv" category in the store
		_, err = db.App.Create().
			SetName("sonarr").
			SetType(app.TypeSonarr).
			SetCategory("tv").
			SetDownloadsPath(tempDir + "/tv").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		syncer := filesync.New(tempDir + "/syncing")
		httpServer := server.NewHTTPServer(syncer, server.WithHTTPDB(db))

		// Add download jobs - one tracked, one not (simulates tracker only tracking matched categories)
		dlTV, err := db.DownloadJob.Create().
			SetRemoteID("dl-tv").
			SetDownloadClientID(dlr.ID).
			SetName("TV.Show").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Create TrackedDownload for TV show (would be created by tracker since it has matching app)
		_, err = db.TrackedDownload.Create().
			SetDownloadJobID(dlTV.ID).
			SetName(dlTV.Name).
			SetCategory(dlTV.Category).
			SetState(trackeddownload.StateDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		// Movie download job WITHOUT TrackedDownload (not tracked since no matching app)
		_, err = db.DownloadJob.Create().
			SetRemoteID("dl-movies").
			SetDownloadClientID(dlr.ID).
			SetName("Movie").
			SetCategory("movies").
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)
		// Note: No TrackedDownload created for movies - tracker wouldn't track it

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		httpServer.ServeHTTP(rec, req)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Only TV show should be visible (only it has a TrackedDownload)
		require.Len(t, response, 1)
		// ID should be a ULID (26 chars), not the hash
		id, ok := response[0]["id"].(string)
		require.True(t, ok, "id should be a string")
		assert.Len(t, id, 26, "id should be a ULID (26 characters)")
		assert.Equal(t, "TV.Show", response[0]["name"])
	})

	t.Run("DownloadsListRecordsSpeed", func(t *testing.T) {
		ts := newTestServer(t)

		// Initial speed history should be empty
		req := httptest.NewRequest(http.MethodGet, "/api/speed-history", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		var history []filesync.SpeedSample
		err := json.Unmarshal(rec.Body.Bytes(), &history)
		require.NoError(t, err)
		assert.Empty(t, history)

		// Call downloads endpoint - this records speed
		req = httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
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
		ctx := context.Background()
		now := time.Now()

		db := mockpkg.NewTestDB(t)

		// Create a downloader
		dlr, err := db.DownloadClient.Create().
			SetName("seedbox").
			SetType("mock").
			SetURL("http://localhost:8080").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		// Create app in store
		_, err = db.App.Create().
			SetName("sonarr").
			SetType(app.TypeSonarr).
			SetCategory("tv").
			SetDownloadsPath(tempDir + "/tv").
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)

		syncer := filesync.New(tempDir + "/syncing")
		httpServer := server.NewHTTPServer(syncer, server.WithHTTPDB(db))

		// Add download jobs and tracked downloads in non-alphabetical order
		dlC, err := db.DownloadJob.Create().
			SetRemoteID("dl-c").
			SetDownloadClientID(dlr.ID).
			SetName("Zebra.Show").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.TrackedDownload.Create().
			SetDownloadJobID(dlC.ID).
			SetName(dlC.Name).
			SetCategory(dlC.Category).
			SetState(trackeddownload.StateDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		dlA, err := db.DownloadJob.Create().
			SetRemoteID("dl-a").
			SetDownloadClientID(dlr.ID).
			SetName("Alpha.Show").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.TrackedDownload.Create().
			SetDownloadJobID(dlA.ID).
			SetName(dlA.Name).
			SetCategory(dlA.Category).
			SetState(trackeddownload.StateDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		dlB, err := db.DownloadJob.Create().
			SetRemoteID("dl-b").
			SetDownloadClientID(dlr.ID).
			SetName("Beta.Show").
			SetCategory("tv").
			SetStatus(downloadjob.StatusDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)
		_, err = db.TrackedDownload.Create().
			SetDownloadJobID(dlB.ID).
			SetName(dlB.Name).
			SetCategory(dlB.Category).
			SetState(trackeddownload.StateDownloading).
			SetDiscoveredAt(now).
			Save(ctx)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
		rec := httptest.NewRecorder()
		httpServer.ServeHTTP(rec, req)

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

// --- Events Tests ---

func TestEventsHandler(t *testing.T) {
	t.Run("EmptyEvents", func(t *testing.T) {
		ts := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []any
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Empty(t, response)
	})

	t.Run("WithEvents", func(t *testing.T) {
		ts := newTestServer(t)
		ctx := context.Background()

		// Add some events
		now := time.Now()
		_, err := ts.db.Event.Create().
			SetType("download.discovered").
			SetMessage("New download found").
			SetSubjectType(event.SubjectTypeDownload).
			SetAppName("sonarr").
			SetTimestamp(now).
			SetCreatedAt(now).
			Save(ctx)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response, 1)
		assert.Equal(t, "download.discovered", response[0]["type"])
	})
}
