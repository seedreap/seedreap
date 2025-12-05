// Package api provides the HTTP API server.
package api //nolint:revive // api is a common, well-understood package name

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/app"
	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	"github.com/seedreap/seedreap/internal/orchestrator"
)

// Server is the HTTP API server.
type Server struct {
	echo         *echo.Echo
	orchestrator *orchestrator.Orchestrator
	downloaders  *download.Registry
	apps         *app.Registry
	syncer       *filesync.Syncer
	logger       zerolog.Logger
	uiFS         fs.FS
}

// Option is a functional option for configuring the server.
type Option func(*Server)

// WithLogger sets the logger.
func WithLogger(logger zerolog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithUI sets the embedded UI filesystem.
func WithUI(uiFS embed.FS, subdir string) Option {
	return func(s *Server) {
		sub, err := fs.Sub(uiFS, subdir)
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to get ui subdirectory")
			return
		}
		s.uiFS = sub
	}
}

// New creates a new API server.
func New(
	orch *orchestrator.Orchestrator,
	downloaders *download.Registry,
	apps *app.Registry,
	syncr *filesync.Syncer,
	opts ...Option,
) *Server {
	s := &Server{
		echo:         echo.New(),
		orchestrator: orch,
		downloaders:  downloaders,
		apps:         apps,
		syncer:       syncr,
		logger:       zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

func (s *Server) setupMiddleware() {
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

func (s *Server) setupRoutes() {
	// API routes
	api := s.echo.Group("/api")

	// Health check
	api.GET("/health", s.healthHandler)

	// Stats
	api.GET("/stats", s.statsHandler)

	// Downloads
	api.GET("/downloads", s.listDownloadsHandler)
	api.GET("/downloads/:id", s.getDownloadHandler)

	// Sync jobs
	api.GET("/jobs", s.listJobsHandler)
	api.GET("/jobs/:id", s.getJobHandler)

	// Speed history for sparkline
	api.GET("/speed-history", s.speedHistoryHandler)

	// Downloaders
	api.GET("/downloaders", s.listDownloadersHandler)

	// Apps
	api.GET("/apps", s.listAppsHandler)

	// Serve UI if available
	if s.uiFS != nil {
		s.echo.GET("/*", echo.WrapHandler(http.FileServer(http.FS(s.uiFS))))
	} else {
		// Serve a basic status page
		s.echo.GET("/", s.indexHandler)
	}
}

// Start starts the server.
func (s *Server) Start(addr string) error {
	s.logger.Info().Str("addr", addr).Msg("starting http server")
	return s.echo.Start(addr)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

// ServeHTTP implements http.Handler for testing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.echo.ServeHTTP(w, r)
}

// Handlers

func (s *Server) healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) statsHandler(c echo.Context) error {
	stats := s.orchestrator.GetStats()
	return c.JSON(http.StatusOK, stats)
}

func (s *Server) listDownloadsHandler(c echo.Context) error {
	tracked := s.orchestrator.GetTrackedDownloads()

	type downloadResponse struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		Downloader   string  `json:"downloader"`
		Category     string  `json:"category"`
		State        string  `json:"state"`
		Progress     float64 `json:"progress"`
		Size         int64   `json:"size"`
		SyncedBytes  int64   `json:"synced_bytes"`
		Error        string  `json:"error,omitempty"`
		DiscoveredAt string  `json:"discovered_at"`
		CompletedAt  string  `json:"completed_at,omitempty"`
	}

	downloads := make([]downloadResponse, 0, len(tracked))
	for _, td := range tracked {
		dl := td.GetDownload()

		// Only include downloads for categories that have matching apps
		if len(s.apps.GetByCategory(dl.Category)) == 0 {
			continue
		}

		state := td.GetState()
		err := td.GetError()
		discoveredAt, completedAt := td.GetTimes()
		job := td.GetSyncJob()

		resp := downloadResponse{
			ID:           dl.ID,
			Name:         dl.Name,
			Downloader:   td.DownloaderName,
			Category:     dl.Category,
			State:        string(state),
			Progress:     dl.Progress,
			Size:         dl.Size,
			DiscoveredAt: discoveredAt.Format("2006-01-02T15:04:05Z07:00"),
		}

		if err != nil {
			resp.Error = err.Error()
		}

		if !completedAt.IsZero() {
			resp.CompletedAt = completedAt.Format("2006-01-02T15:04:05Z07:00")
		}

		if job != nil {
			syncedBytes, _ := job.GetProgress()
			resp.SyncedBytes = syncedBytes
		}

		downloads = append(downloads, resp)
	}

	// Sort by name for consistent ordering
	sort.Slice(downloads, func(i, j int) bool {
		return downloads[i].Name < downloads[j].Name
	})

	return c.JSON(http.StatusOK, downloads)
}

func (s *Server) getDownloadHandler(c echo.Context) error {
	id := c.Param("id")

	tracked := s.orchestrator.GetTrackedDownloads()
	for _, td := range tracked {
		dl := td.GetDownload()
		if dl.ID == id {
			state := td.GetState()
			job := td.GetSyncJob()

			resp := map[string]any{
				"id":         dl.ID,
				"name":       dl.Name,
				"downloader": td.DownloaderName,
				"category":   dl.Category,
				"state":      string(state),
				"progress":   dl.Progress,
				"size":       dl.Size,
				"save_path":  dl.SavePath,
			}

			if job != nil {
				snapshot := job.Snapshot()
				files := make([]map[string]any, 0, len(snapshot.Files))
				for _, f := range snapshot.Files {
					files = append(files, map[string]any{
						"path":          f.Path,
						"size":          f.Size,
						"transferred":   f.Transferred,
						"status":        string(f.Status),
						"bytes_per_sec": f.BytesPerSec,
					})
				}
				resp["files"] = files
			}

			return c.JSON(http.StatusOK, resp)
		}
	}

	return c.JSON(http.StatusNotFound, map[string]string{
		"error": "download not found",
	})
}

func (s *Server) listJobsHandler(c echo.Context) error {
	type jobResponse struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Downloader      string  `json:"downloader"`
		Category        string  `json:"category"`
		Status          string  `json:"status"`
		SeedboxState    string  `json:"seedbox_state"`
		SeedboxProgress float64 `json:"seedbox_progress"`
		TotalSize       int64   `json:"total_size"`
		CompletedSize   int64   `json:"completed_size"`
		TotalFiles      int     `json:"total_files"`
		BytesPerSec     int64   `json:"bytes_per_sec"`
	}

	response := make([]jobResponse, 0)

	// Add all tracked downloads (includes those with and without sync jobs)
	for _, td := range s.orchestrator.GetTrackedDownloads() {
		dl := td.GetDownload()

		// Only include downloads for categories that have matching apps
		if len(s.apps.GetByCategory(dl.Category)) == 0 {
			continue
		}

		job := td.GetSyncJob()
		state := td.GetState()

		var resp jobResponse
		if job != nil {
			// Has a sync job - use its data
			snapshot := job.Snapshot()
			resp = jobResponse{
				ID:              snapshot.ID,
				Name:            snapshot.Name,
				Downloader:      snapshot.Downloader,
				Category:        snapshot.Category,
				Status:          string(snapshot.Status),
				SeedboxState:    string(dl.State),
				SeedboxProgress: dl.Progress,
				TotalSize:       snapshot.TotalSize,
				CompletedSize:   snapshot.CompletedSize,
				TotalFiles:      snapshot.TotalFiles,
				BytesPerSec:     snapshot.BytesPerSec,
			}
		} else {
			// No sync job - use tracked download data
			// This happens when files already exist at final destination or still downloading
			resp = jobResponse{
				ID:              dl.ID,
				Name:            dl.Name,
				Downloader:      td.DownloaderName,
				Category:        dl.Category,
				Status:          string(state), // Use orchestrator state
				SeedboxState:    string(dl.State),
				SeedboxProgress: dl.Progress,
				TotalSize:       dl.Size,
				CompletedSize:   0, // No sync progress yet
				TotalFiles:      len(dl.Files),
				BytesPerSec:     0, // No active transfer
			}

			// If state is complete, files are done
			if state == orchestrator.StateComplete {
				resp.CompletedSize = dl.Size
			}
		}

		response = append(response, resp)
	}

	// Record total speed for sparkline history from the transferer
	s.syncer.RecordSpeed(s.syncer.GetAggregateSpeed())

	// Sort by name for consistent ordering
	sort.Slice(response, func(i, j int) bool {
		return response[i].Name < response[j].Name
	})

	return c.JSON(http.StatusOK, response)
}

//nolint:gocognit // handler has multiple code paths for different data sources
func (s *Server) getJobHandler(c echo.Context) error {
	id := c.Param("id")

	// First try to get from syncer (has detailed file info)
	job, ok := s.syncer.GetJob(id)
	if ok {
		snapshot := job.Snapshot()

		files := make([]map[string]any, 0, len(snapshot.Files))
		for _, f := range snapshot.Files {
			files = append(files, map[string]any{
				"path":          f.Path,
				"size":          f.Size,
				"transferred":   f.Transferred,
				"status":        string(f.Status),
				"bytes_per_sec": f.BytesPerSec,
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"id":             snapshot.ID,
			"name":           snapshot.Name,
			"downloader":     snapshot.Downloader,
			"category":       snapshot.Category,
			"status":         string(snapshot.Status),
			"total_size":     snapshot.TotalSize,
			"completed_size": snapshot.CompletedSize,
			"total_files":    snapshot.TotalFiles,
			"remote_base":    snapshot.RemoteBase,
			"local_base":     snapshot.LocalBase,
			"final_path":     snapshot.FinalPath,
			"files":          files,
		})
	}

	// Fall back to tracked download (for completed downloads without sync job)
	for _, td := range s.orchestrator.GetTrackedDownloads() {
		dl := td.GetDownload()
		if dl.ID == id { //nolint:nestif // building response requires nested conditions
			state := td.GetState()

			// Build file list from download info
			files := make([]map[string]any, 0, len(dl.Files))
			var totalDownloaded int64
			for _, f := range dl.Files {
				if f.Priority == 0 {
					continue
				}

				// Map file state to status string
				status := string(f.State) // "downloading" or "complete"
				if f.State == "" {
					// Fallback if state not set
					if state == orchestrator.StateComplete {
						status = "complete"
					} else {
						status = "pending"
					}
				}

				files = append(files, map[string]any{
					"path":          f.Path,
					"size":          f.Size,
					"transferred":   f.Downloaded,
					"status":        status,
					"bytes_per_sec": int64(0),
				})
				totalDownloaded += f.Downloaded
			}

			return c.JSON(http.StatusOK, map[string]any{
				"id":             dl.ID,
				"name":           dl.Name,
				"downloader":     td.DownloaderName,
				"category":       dl.Category,
				"status":         string(state),
				"total_size":     dl.Size,
				"completed_size": totalDownloaded,
				"total_files":    len(files),
				"files":          files,
			})
		}
	}

	return c.JSON(http.StatusNotFound, map[string]string{
		"error": "job not found",
	})
}

func (s *Server) listDownloadersHandler(c echo.Context) error {
	downloaders := s.downloaders.All()

	response := make([]map[string]string, 0, len(downloaders))
	for name, dl := range downloaders {
		response = append(response, map[string]string{
			"name": name,
			"type": dl.Type(),
		})
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) listAppsHandler(c echo.Context) error {
	apps := s.apps.All()

	response := make([]map[string]string, 0, len(apps))
	for name, a := range apps {
		response = append(response, map[string]string{
			"name":     name,
			"type":     a.Type(),
			"category": a.Category(),
		})
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) indexHandler(c echo.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>SeedReap</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 40px; }
        h1 { color: #333; }
        .status { color: #28a745; }
        a { color: #007bff; }
    </style>
</head>
<body>
    <h1>SeedReap</h1>
    <p class="status">Status: Running</p>
    <h2>API Endpoints</h2>
    <ul>
        <li><a href="/api/health">/api/health</a> - Health check</li>
        <li><a href="/api/stats">/api/stats</a> - Statistics</li>
        <li><a href="/api/downloads">/api/downloads</a> - List tracked downloads</li>
        <li><a href="/api/jobs">/api/jobs</a> - List sync jobs</li>
        <li><a href="/api/downloaders">/api/downloaders</a> - List configured downloaders</li>
        <li><a href="/api/apps">/api/apps</a> - List configured apps</li>
    </ul>
</body>
</html>`
	return c.HTML(http.StatusOK, html)
}

func (s *Server) speedHistoryHandler(c echo.Context) error {
	history := s.syncer.GetSpeedHistory()
	return c.JSON(http.StatusOK, history)
}
