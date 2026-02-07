package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/app"
)

// --- Passthrough Tests ---

func TestPassthrough(t *testing.T) {
	t.Run("NewPassthrough", func(t *testing.T) {
		p := app.NewPassthrough("misc", "misc-category", "/downloads/misc")

		assert.Equal(t, "misc", p.Name())
		assert.Equal(t, "passthrough", p.Type())
		assert.Equal(t, "misc-category", p.Category())
		assert.Equal(t, "/downloads/misc", p.DownloadsPath())
		assert.False(t, p.CleanupOnCategoryChange())
		assert.False(t, p.CleanupOnRemove())
	})

	t.Run("WithOptions", func(t *testing.T) {
		p := app.NewPassthrough(
			"misc",
			"misc-category",
			"/downloads/misc",
			app.WithLogger(zerolog.Nop()),
			app.WithCleanupOnCategoryChange(true),
			app.WithCleanupOnRemove(true),
		)

		assert.True(t, p.CleanupOnCategoryChange())
		assert.True(t, p.CleanupOnRemove())
	})

	t.Run("TriggerImport_NoOp", func(t *testing.T) {
		p := app.NewPassthrough("misc", "misc", "/downloads/misc")

		err := p.TriggerImport(context.Background(), "/some/path")
		assert.NoError(t, err, "passthrough TriggerImport should always succeed")
	})

	t.Run("TestConnection_AlwaysSucceeds", func(t *testing.T) {
		p := app.NewPassthrough("misc", "misc", "/downloads/misc")

		err := p.TestConnection(context.Background())
		assert.NoError(t, err, "passthrough TestConnection should always succeed")
	})
}

// --- Sonarr Tests ---

func TestSonarr(t *testing.T) {
	t.Run("NewSonarr", func(t *testing.T) {
		cfg := app.ArrConfig{
			URL:           "http://sonarr:8989",
			APIKey:        "test-api-key",
			Category:      "tv-sonarr",
			DownloadsPath: "/downloads/tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		assert.Equal(t, "sonarr", s.Name())
		assert.Equal(t, "sonarr", s.Type())
		assert.Equal(t, "tv-sonarr", s.Category())
		assert.Equal(t, "/downloads/tv", s.DownloadsPath())
		assert.False(t, s.CleanupOnCategoryChange())
		assert.False(t, s.CleanupOnRemove())
	})

	t.Run("WithOptions", func(t *testing.T) {
		cfg := app.ArrConfig{
			URL:      "http://sonarr:8989",
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr(
			"sonarr",
			cfg,
			app.WithLogger(zerolog.Nop()),
			app.WithCleanupOnCategoryChange(true),
			app.WithCleanupOnRemove(true),
		)

		assert.True(t, s.CleanupOnCategoryChange())
		assert.True(t, s.CleanupOnRemove())
	})

	t.Run("TriggerImport_Success", func(t *testing.T) {
		var receivedRequest map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/command", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "test-api-key", r.Header.Get("X-Api-Key"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			err := json.NewDecoder(r.Body).Decode(&receivedRequest)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 1}`))
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TriggerImport(context.Background(), "/downloads/tv/Show.S01E01")
		require.NoError(t, err)

		assert.Equal(t, "DownloadedEpisodesScan", receivedRequest["name"])
		assert.Equal(t, "/downloads/tv/Show.S01E01", receivedRequest["path"])
	})

	t.Run("TriggerImport_NoPath", func(t *testing.T) {
		var receivedRequest map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := json.NewDecoder(r.Body).Decode(&receivedRequest)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TriggerImport(context.Background(), "")
		require.NoError(t, err)

		assert.Equal(t, "DownloadedEpisodesScan", receivedRequest["name"])
		assert.Nil(t, receivedRequest["path"], "path should not be set when empty")
	})

	t.Run("TriggerImport_HTTPError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "internal error"}`))
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TriggerImport(context.Background(), "/downloads/tv/Show")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("TestConnection_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/system/status", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "test-api-key", r.Header.Get("X-Api-Key"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version": "4.0.0", "appName": "Sonarr"}`))
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TestConnection(context.Background())
		assert.NoError(t, err)
	})

	t.Run("TestConnection_Unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "wrong-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TestConnection(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("TestConnection_NetworkError", func(t *testing.T) {
		cfg := app.ArrConfig{
			URL:      "http://localhost:99999", // Invalid port
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TestConnection(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})
}

// --- Radarr Tests ---

func TestRadarr(t *testing.T) {
	t.Run("NewRadarr", func(t *testing.T) {
		cfg := app.ArrConfig{
			URL:           "http://radarr:7878",
			APIKey:        "test-api-key",
			Category:      "movies-radarr",
			DownloadsPath: "/downloads/movies",
		}

		r := app.NewRadarr("radarr", cfg)

		assert.Equal(t, "radarr", r.Name())
		assert.Equal(t, "radarr", r.Type())
		assert.Equal(t, "movies-radarr", r.Category())
		assert.Equal(t, "/downloads/movies", r.DownloadsPath())
	})

	t.Run("TriggerImport_UsesCorrectScanCommand", func(t *testing.T) {
		var receivedRequest map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := json.NewDecoder(r.Body).Decode(&receivedRequest)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		cfg := app.ArrConfig{
			URL:      server.URL,
			APIKey:   "test-api-key",
			Category: "movies",
		}

		r := app.NewRadarr("radarr", cfg)

		err := r.TriggerImport(context.Background(), "/downloads/movies/Movie.2024")
		require.NoError(t, err)

		// Radarr should use DownloadedMoviesScan instead of DownloadedEpisodesScan
		assert.Equal(t, "DownloadedMoviesScan", receivedRequest["name"])
		assert.Equal(t, "/downloads/movies/Movie.2024", receivedRequest["path"])
	})
}

// --- URL Normalization Tests ---

func TestArrClient_URLNormalization(t *testing.T) {
	t.Run("TrailingSlashRemoved", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify path doesn't have double slashes
			assert.Equal(t, "/api/v3/system/status", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version": "4.0.0"}`))
		}))
		defer server.Close()

		// Add trailing slash to URL
		cfg := app.ArrConfig{
			URL:      server.URL + "/",
			APIKey:   "test-api-key",
			Category: "tv",
		}

		s := app.NewSonarr("sonarr", cfg)

		err := s.TestConnection(context.Background())
		assert.NoError(t, err)
	})
}
