package server

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"sort"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/apitypes"
	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/downloadfile"
	"github.com/seedreap/seedreap/internal/ent/generated/event"
	"github.com/seedreap/seedreap/internal/ent/generated/predicate"
	"github.com/seedreap/seedreap/internal/ent/generated/syncfile"
	"github.com/seedreap/seedreap/internal/ent/generated/trackeddownload"
	"github.com/seedreap/seedreap/internal/filesync"
)

// validIDPattern matches valid ID formats: alphanumeric, hyphens, underscores.
// This is intentionally permissive to support various downloader ID formats
// (hashes, UUIDs, numeric IDs, etc.) while blocking path traversal and injection.
var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// maxIDLength is the maximum allowed length for ID parameters.
const maxIDLength = 256

// defaultEventsLimit is the maximum number of events to return.
const defaultEventsLimit = 100

// validateID checks that an ID parameter is non-empty, reasonable length,
// and contains only safe characters.
func validateID(id string) error {
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}
	if len(id) > maxIDLength {
		return echo.NewHTTPError(http.StatusBadRequest, "id too long")
	}
	if !validIDPattern.MatchString(id) {
		return echo.NewHTTPError(http.StatusBadRequest, "id contains invalid characters")
	}
	return nil
}

// HTTPServer is the HTTP API server.
type HTTPServer struct {
	echo   *echo.Echo
	syncer *filesync.Syncer
	db     *generated.Client
	logger zerolog.Logger
	uiFS   fs.FS
}

// HTTPOption is a functional option for configuring the HTTP server.
type HTTPOption func(*HTTPServer)

// WithHTTPLogger sets the logger.
func WithHTTPLogger(logger zerolog.Logger) HTTPOption {
	return func(s *HTTPServer) {
		s.logger = logger
	}
}

// WithUI sets the embedded UI filesystem.
func WithUI(uiFS embed.FS, subdir string) HTTPOption {
	return func(s *HTTPServer) {
		sub, err := fs.Sub(uiFS, subdir)
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to get ui subdirectory")
			return
		}
		s.uiFS = sub
	}
}

// WithHTTPDB sets the database client.
func WithHTTPDB(db *generated.Client) HTTPOption {
	return func(s *HTTPServer) {
		s.db = db
	}
}

// NewHTTPServer creates a new HTTP API server.
func NewHTTPServer(
	syncr *filesync.Syncer,
	opts ...HTTPOption,
) *HTTPServer {
	s := &HTTPServer{
		echo:   echo.New(),
		syncer: syncr,
		logger: zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

func (s *HTTPServer) setupMiddleware() {
	s.echo.HideBanner = true
	s.echo.HidePort = true

	// Request logging
	s.echo.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogMethod: true,
		LogError:  true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error != nil {
				s.logger.Error().
					Err(v.Error).
					Str("method", v.Method).
					Str("uri", v.URI).
					Int("status", v.Status).
					Msg("request error")
			} else {
				s.logger.Debug().
					Str("method", v.Method).
					Str("uri", v.URI).
					Int("status", v.Status).
					Msg("request")
			}
			return nil
		},
	}))

	// Recovery
	s.echo.Use(middleware.Recover())

	// CORS
	s.echo.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))
}

func (s *HTTPServer) setupRoutes() {
	// API routes
	api := s.echo.Group("/api")

	// Health check
	api.GET("/health", s.healthHandler)

	// Stats
	api.GET("/stats", s.statsHandler)

	// Downloads - flat routes using ULID-based IDs
	api.GET("/downloads", s.listDownloadsHandler)
	api.GET("/downloads/:id", s.getDownloadHandler)
	api.GET("/downloads/:id/events", s.downloadEventsHandler)

	// Speed history for sparkline
	api.GET("/speed-history", s.speedHistoryHandler)

	// Downloaders
	api.GET("/downloaders", s.listDownloadersHandler)
	api.GET("/downloaders/:id/events", s.downloaderEventsHandler)

	// Apps
	api.GET("/apps", s.listAppsHandler)
	api.GET("/apps/:id/events", s.appEventsHandler)

	// Events
	api.GET("/events", s.eventsHandler)

	// Serve UI if available
	if s.uiFS != nil {
		s.echo.GET("/*", echo.WrapHandler(http.FileServer(http.FS(s.uiFS))))
	}
}

// Start starts the server.
func (s *HTTPServer) Start(addr string) error {
	s.logger.Info().Str("addr", addr).Msg("starting http server")
	return s.echo.Start(addr)
}

// Shutdown gracefully shuts down the server.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

// ServeHTTP implements http.Handler for testing.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.echo.ServeHTTP(w, r)
}

// Handlers

func (s *HTTPServer) healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, apitypes.HealthResponse{
		Status: "ok",
	})
}

func (s *HTTPServer) statsHandler(c echo.Context) error {
	resp := apitypes.Stats{
		ByState: make(map[string]int),
	}

	if s.db == nil {
		return c.JSON(http.StatusOK, resp)
	}

	ctx := c.Request().Context()

	// Get all tracked downloads - this is the single source of truth for stats
	trackedDownloads, err := s.db.TrackedDownload.Query().All(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get tracked downloads for stats")
		return c.JSON(http.StatusOK, resp)
	}

	resp.TotalTracked = len(trackedDownloads)

	// Count by TrackedDownload state
	for _, td := range trackedDownloads {
		state := string(td.State)
		resp.ByState[state]++

		// Count special seedbox states
		switch td.State {
		case trackeddownload.StateDownloading, trackeddownload.StateDownloadingSyncing:
			resp.DownloadingOnSeedbox++
		case trackeddownload.StatePaused:
			resp.PausedOnSeedbox++
		default:
			// Other states don't contribute to seedbox counters
		}
	}

	return c.JSON(http.StatusOK, resp)
}

func (s *HTTPServer) listDownloadsHandler(c echo.Context) error {
	if s.db == nil {
		return c.JSON(http.StatusOK, []apitypes.Download{})
	}

	ctx := c.Request().Context()

	// Get tracked downloads with preloaded edges
	trackedDownloads, err := s.db.TrackedDownload.Query().
		WithDownloadJob(func(q *generated.DownloadJobQuery) {
			q.WithDownloadClient()
			q.WithSyncJob()
			q.WithMoveJob()
			q.WithAppJobs()
		}).
		All(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list tracked downloads")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to list downloads",
		})
	}

	downloads := make([]apitypes.Download, 0, len(trackedDownloads))
	for _, td := range trackedDownloads {
		// Build comprehensive download view from TrackedDownload
		resp := s.buildDownloadResponse(ctx, td, false)
		downloads = append(downloads, resp)
	}

	// Record total speed for sparkline history from the transferer
	s.syncer.RecordSpeed(s.syncer.GetAggregateSpeed())

	// Sort by name for consistent ordering
	sort.Slice(downloads, func(i, j int) bool {
		return downloads[i].Name < downloads[j].Name
	})

	return c.JSON(http.StatusOK, downloads)
}

// buildDownloadResponse constructs a comprehensive Download API response from a TrackedDownload.
// includeFiles determines whether to include file-level detail.
// The TrackedDownload must have its DownloadJob edge preloaded.
func (s *HTTPServer) buildDownloadResponse(
	ctx context.Context,
	td *generated.TrackedDownload,
	includeFiles bool,
) apitypes.Download {
	// Get preloaded DownloadJob
	dl := td.Edges.DownloadJob
	if dl == nil {
		s.logger.Error().Str("tracked_download_id", td.ID.String()).Msg("download job edge not loaded")
		return apitypes.Download{
			ID:    td.ID.String(),
			Name:  td.Name,
			State: string(td.State),
			Error: "internal error: download job not loaded",
		}
	}

	// Get downloader name from edge
	downloaderName := ""
	if dlr := dl.Edges.DownloadClient; dlr != nil {
		downloaderName = dlr.Name
	}

	resp := apitypes.Download{
		ID:            td.ID.String(),
		Name:          td.Name,
		Downloader:    downloaderName,
		Category:      td.Category,
		App:           td.AppName,
		TotalSize:     td.TotalSize,
		CompletedSize: td.CompletedSize,
		TotalFiles:    td.TotalFiles,
		DiscoveredAt:  td.DiscoveredAt.Format(timeFormat),
		// Use state directly from TrackedDownload (computed by tracker controller)
		State: string(td.State),
		Error: td.ErrorMessage,
	}

	// Build download job info
	resp.DownloadJob = s.buildDownloadJobInfo(dl)

	// Build sync job info from preloaded edge
	if sj := dl.Edges.SyncJob; sj != nil {
		resp.SyncJob = s.buildSyncJobInfo(sj)
	}

	// Build move job info from preloaded edge
	if mj := dl.Edges.MoveJob; mj != nil {
		resp.MoveJob = s.buildMoveJobInfo(mj)
	}

	// Build app jobs info from preloaded edge
	if appJobs := dl.Edges.AppJobs; len(appJobs) > 0 {
		resp.AppJobs = s.buildAppJobsInfo(appJobs)
	}

	// Get live sync data from syncer for real-time speed and progress
	if syncDownload, ok := s.syncer.GetSyncDownload(dl.DownloadClientID.String(), dl.RemoteID); ok {
		snapshot := syncDownload.Snapshot()
		// Use live data for real-time values
		resp.CompletedSize = snapshot.CompletedSize
		resp.TotalFiles = snapshot.TotalFiles
		resp.BytesPerSec = snapshot.BytesPerSec

		if includeFiles {
			resp.Files = s.buildFilesFromSyncer(ctx, dl, dl.Edges.SyncJob, snapshot)
		}
	} else if includeFiles {
		resp.Files = s.buildFilesFromStore(ctx, dl, dl.Edges.SyncJob)
	}

	// Ensure TotalFiles has a value
	if resp.TotalFiles == 0 {
		downloadFiles, _ := s.db.DownloadFile.Query().
			Where(downloadfile.DownloadJobIDEQ(dl.ID)).
			All(ctx)
		resp.TotalFiles = len(downloadFiles)
	}

	return resp
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

func (s *HTTPServer) buildDownloadJobInfo(dl *generated.DownloadJob) *apitypes.DownloadJobInfo {
	info := &apitypes.DownloadJobInfo{
		Status:        string(dl.Status),
		Progress:      dl.Progress,
		Size:          dl.Size,
		Downloaded:    dl.Downloaded,
		DownloadSpeed: dl.DownloadSpeed,
		SavePath:      dl.SavePath,
	}
	if dl.DownloadedAt != nil {
		info.CompletedAt = dl.DownloadedAt.Format(timeFormat)
	}
	if dl.ErrorMessage != "" {
		info.Error = dl.ErrorMessage
	}
	return info
}

func (s *HTTPServer) buildSyncJobInfo(sj *generated.SyncJob) *apitypes.SyncJobInfo {
	info := &apitypes.SyncJobInfo{
		ID:         sj.ID.String(),
		Status:     string(sj.Status),
		RemoteBase: sj.RemoteBase,
		LocalBase:  sj.LocalBase,
	}
	if sj.StartedAt != nil {
		info.StartedAt = sj.StartedAt.Format(timeFormat)
	}
	if sj.CompletedAt != nil {
		info.CompletedAt = sj.CompletedAt.Format(timeFormat)
	}
	if sj.ErrorMessage != "" {
		info.Error = sj.ErrorMessage
	}
	return info
}

func (s *HTTPServer) buildMoveJobInfo(mj *generated.MoveJob) *apitypes.MoveJobInfo {
	info := &apitypes.MoveJobInfo{
		ID:              mj.ID.String(),
		Status:          string(mj.Status),
		SourcePath:      mj.SourcePath,
		DestinationPath: mj.DestinationPath,
	}
	if mj.StartedAt != nil {
		info.StartedAt = mj.StartedAt.Format(timeFormat)
	}
	if mj.CompletedAt != nil {
		info.CompletedAt = mj.CompletedAt.Format(timeFormat)
	}
	if mj.ErrorMessage != "" {
		info.Error = mj.ErrorMessage
	}
	return info
}

func (s *HTTPServer) buildAppJobsInfo(appJobs []*generated.AppJob) []apitypes.AppJobInfo {
	result := make([]apitypes.AppJobInfo, 0, len(appJobs))
	for _, aj := range appJobs {
		info := apitypes.AppJobInfo{
			ID:      aj.ID.String(),
			AppName: aj.AppName,
			Status:  string(aj.Status),
			Path:    aj.Path,
		}
		if aj.StartedAt != nil {
			info.StartedAt = aj.StartedAt.Format(timeFormat)
		}
		if aj.CompletedAt != nil {
			info.CompletedAt = aj.CompletedAt.Format(timeFormat)
		}
		if aj.ErrorMessage != "" {
			info.Error = aj.ErrorMessage
		}
		result = append(result, info)
	}
	return result
}

// buildFilesFromSyncer builds the file list from active syncer data plus store data.
func (s *HTTPServer) buildFilesFromSyncer(
	ctx context.Context,
	dl *generated.DownloadJob,
	sj *generated.SyncJob,
	snapshot filesync.SyncDownloadSnapshot,
) []apitypes.File {
	// Get download files from database
	downloadFiles, _ := s.db.DownloadFile.Query().
		Where(downloadfile.DownloadJobIDEQ(dl.ID)).
		All(ctx)
	downloadFileMap := make(map[string]*generated.DownloadFile)
	for _, df := range downloadFiles {
		downloadFileMap[df.RelativePath] = df
	}

	// Get sync files from database
	var syncFileMap map[string]*generated.SyncFile
	if sj != nil {
		syncFiles, _ := s.db.SyncFile.Query().
			Where(syncfile.SyncJobIDEQ(sj.ID)).
			All(ctx)
		syncFileMap = make(map[string]*generated.SyncFile)
		for _, sf := range syncFiles {
			syncFileMap[sf.RelativePath] = sf
		}
	}

	files := make([]apitypes.File, 0, len(snapshot.Files))
	for _, f := range snapshot.Files {
		apiFile := apitypes.File{
			Path:        f.Path,
			Size:        f.Size,
			SyncedSize:  f.Transferred,
			SyncStatus:  string(f.Status),
			BytesPerSec: f.BytesPerSec,
		}

		// Add download file info
		if df := downloadFileMap[f.Path]; df != nil {
			apiFile.Downloaded = df.Downloaded
			apiFile.DownloadProgress = df.Progress
			apiFile.Priority = df.Priority
		}

		// Add sync file error if any
		if sf := syncFileMap[f.Path]; sf != nil && sf.ErrorMessage != "" {
			apiFile.SyncError = sf.ErrorMessage
		}

		files = append(files, apiFile)
	}
	return files
}

// buildFilesFromStore builds the file list from store data only (no active sync).
func (s *HTTPServer) buildFilesFromStore(
	ctx context.Context,
	dl *generated.DownloadJob,
	sj *generated.SyncJob,
) []apitypes.File {
	// Get download files from database
	downloadFiles, _ := s.db.DownloadFile.Query().
		Where(downloadfile.DownloadJobIDEQ(dl.ID)).
		All(ctx)
	if len(downloadFiles) == 0 {
		return nil
	}

	// Get sync files from database if sync job exists
	var syncFileMap map[string]*generated.SyncFile
	if sj != nil {
		syncFiles, _ := s.db.SyncFile.Query().
			Where(syncfile.SyncJobIDEQ(sj.ID)).
			All(ctx)
		syncFileMap = make(map[string]*generated.SyncFile)
		for _, sf := range syncFiles {
			syncFileMap[sf.RelativePath] = sf
		}
	}

	files := make([]apitypes.File, 0, len(downloadFiles))
	for _, df := range downloadFiles {
		apiFile := apitypes.File{
			Path:             df.RelativePath,
			Size:             df.Size,
			Downloaded:       df.Downloaded,
			DownloadProgress: df.Progress,
			Priority:         df.Priority,
		}

		// Add sync file info if available
		if sf := syncFileMap[df.RelativePath]; sf != nil {
			apiFile.SyncedSize = sf.SyncedSize
			apiFile.SyncStatus = string(sf.Status)
			if sf.ErrorMessage != "" {
				apiFile.SyncError = sf.ErrorMessage
			}
		}

		files = append(files, apiFile)
	}
	return files
}

func (s *HTTPServer) getDownloadHandler(c echo.Context) error {
	idStr := c.Param("id")
	if err := validateID(idStr); err != nil {
		return err
	}

	// Parse ULID
	id, err := ulid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, apitypes.ErrorResponse{
			Error: "invalid download id",
		})
	}

	if s.db == nil {
		return c.JSON(http.StatusNotFound, apitypes.ErrorResponse{
			Error: "download not found",
		})
	}

	ctx := c.Request().Context()

	// Get tracked download by ID with preloaded edges
	td, err := s.db.TrackedDownload.Query().
		Where(trackeddownload.IDEQ(id)).
		WithDownloadJob(func(q *generated.DownloadJobQuery) {
			q.WithSyncJob()
			q.WithMoveJob()
			q.WithAppJobs()
		}).
		Only(ctx)
	if err != nil {
		return c.JSON(http.StatusNotFound, apitypes.ErrorResponse{
			Error: "download not found",
		})
	}

	// Build comprehensive download view with files
	resp := s.buildDownloadResponse(ctx, td, true)

	return c.JSON(http.StatusOK, resp)
}

func (s *HTTPServer) listDownloadersHandler(c echo.Context) error {
	if s.db == nil {
		return c.JSON(http.StatusOK, []apitypes.DownloadClient{})
	}

	downloaders, err := s.db.DownloadClient.Query().All(c.Request().Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list downloaders from store")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to list downloaders",
		})
	}

	response := make([]apitypes.DownloadClient, 0, len(downloaders))
	for _, d := range downloaders {
		response = append(response, entDownloadClientToAPIType(d))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *HTTPServer) listAppsHandler(c echo.Context) error {
	if s.db == nil {
		return c.JSON(http.StatusOK, []apitypes.App{})
	}

	apps, err := s.db.App.Query().All(c.Request().Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list apps from store")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to list apps",
		})
	}

	response := make([]apitypes.App, 0, len(apps))
	for _, a := range apps {
		response = append(response, entAppToAPIType(a))
	}

	return c.JSON(http.StatusOK, response)
}

func (s *HTTPServer) speedHistoryHandler(c echo.Context) error {
	history := s.syncer.GetSpeedHistory()

	// Convert to apitypes.SpeedSample for API response
	response := make([]apitypes.SpeedSample, len(history))
	for i, sample := range history {
		response[i] = apitypes.SpeedSample{
			Speed:     sample.Speed,
			Timestamp: sample.Timestamp,
		}
	}

	return c.JSON(http.StatusOK, response)
}

func (s *HTTPServer) eventsHandler(c echo.Context) error {
	if s.db == nil {
		return c.JSON(http.StatusOK, []*generated.Event{})
	}

	events, err := s.db.Event.Query().
		Order(event.ByTimestamp()).
		Limit(defaultEventsLimit).
		All(c.Request().Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get events")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to get events",
		})
	}

	return c.JSON(http.StatusOK, events)
}

func (s *HTTPServer) appEventsHandler(c echo.Context) error {
	id := c.Param("id")
	if err := validateID(id); err != nil {
		return err
	}

	if s.db == nil {
		return c.JSON(http.StatusOK, []*generated.Event{})
	}

	events, err := s.db.Event.Query().
		Where(event.AppNameEQ(id)).
		Order(event.ByTimestamp()).
		Limit(defaultEventsLimit).
		All(c.Request().Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get app events")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to get events",
		})
	}

	return c.JSON(http.StatusOK, events)
}

func (s *HTTPServer) downloaderEventsHandler(c echo.Context) error {
	idStr := c.Param("id")
	if err := validateID(idStr); err != nil {
		return err
	}

	// Parse ULID
	id, err := ulid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, apitypes.ErrorResponse{
			Error: "invalid downloader id",
		})
	}

	if s.db == nil {
		return c.JSON(http.StatusOK, []*generated.Event{})
	}

	events, err := s.db.Event.Query().
		Where(
			event.SubjectTypeEQ(event.SubjectTypeDownloader),
			event.SubjectIDEQ(id.String()),
		).
		Order(event.ByTimestamp()).
		Limit(defaultEventsLimit).
		All(c.Request().Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get downloader events")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to get events",
		})
	}

	return c.JSON(http.StatusOK, events)
}

func (s *HTTPServer) downloadEventsHandler(c echo.Context) error {
	idStr := c.Param("id")
	if err := validateID(idStr); err != nil {
		return err
	}

	// Parse ULID
	id, err := ulid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, apitypes.ErrorResponse{
			Error: "invalid download id",
		})
	}

	if s.db == nil {
		return c.JSON(http.StatusOK, []*generated.Event{})
	}

	ctx := c.Request().Context()

	// Get tracked download with all related jobs to collect their IDs
	td, err := s.db.TrackedDownload.Query().
		Where(trackeddownload.IDEQ(id)).
		WithDownloadJob(func(q *generated.DownloadJobQuery) {
			q.WithSyncJob()
			q.WithMoveJob()
			q.WithAppJobs()
		}).
		Only(ctx)
	if err != nil {
		return c.JSON(http.StatusNotFound, apitypes.ErrorResponse{
			Error: "download not found",
		})
	}

	// Build predicates for all related entities
	predicates := []predicate.Event{
		// Events for the tracked download itself
		event.And(
			event.SubjectTypeEQ(event.SubjectTypeDownload),
			event.SubjectIDEQ(td.ID.String()),
		),
	}

	// Add predicates for related jobs
	if dl := td.Edges.DownloadJob; dl != nil {
		if sj := dl.Edges.SyncJob; sj != nil {
			predicates = append(predicates, event.And(
				event.SubjectTypeEQ(event.SubjectTypeSyncJob),
				event.SubjectIDEQ(sj.ID.String()),
			))
		}
		if mj := dl.Edges.MoveJob; mj != nil {
			predicates = append(predicates, event.And(
				event.SubjectTypeEQ(event.SubjectTypeMoveJob),
				event.SubjectIDEQ(mj.ID.String()),
			))
		}
		for _, aj := range dl.Edges.AppJobs {
			predicates = append(predicates, event.And(
				event.SubjectTypeEQ(event.SubjectTypeAppJob),
				event.SubjectIDEQ(aj.ID.String()),
			))
		}
	}

	events, err := s.db.Event.Query().
		Where(event.Or(predicates...)).
		Order(event.ByTimestamp()).
		Limit(defaultEventsLimit).
		All(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get download events")
		return c.JSON(http.StatusInternalServerError, apitypes.ErrorResponse{
			Error: "failed to get events",
		})
	}

	return c.JSON(http.StatusOK, events)
}

// Type conversion helpers

// entAppToAPIType converts an Ent App to an apitypes.App.
func entAppToAPIType(a *generated.App) apitypes.App {
	return apitypes.App{
		Name:     a.Name,
		Type:     string(a.Type),
		Category: a.Category,
	}
}

// entDownloadClientToAPIType converts an Ent DownloadClient to an apitypes.DownloadClient.
func entDownloadClientToAPIType(d *generated.DownloadClient) apitypes.DownloadClient {
	return apitypes.DownloadClient{
		Name: d.Name,
		Type: d.Type,
	}
}
