package timeline_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/timeline"
)

func TestNewRecorder(t *testing.T) {
	t.Run("creates recorder with defaults", func(t *testing.T) {
		r := timeline.NewRecorder()
		require.NotNil(t, r)

		events := r.GetAll()
		assert.Empty(t, events)
	})

	t.Run("creates recorder with custom max events", func(t *testing.T) {
		r := timeline.NewRecorder(timeline.WithMaxEvents(5))
		require.NotNil(t, r)

		// Add 10 events
		for range 10 {
			r.Record(timeline.Event{
				Type:    timeline.EventDiscovered,
				Message: "test",
			})
		}

		events := r.GetAll()
		assert.Len(t, events, 5)
	})
}

func TestRecorder_Record(t *testing.T) {
	t.Run("records event with generated ID and timestamp", func(t *testing.T) {
		r := timeline.NewRecorder()

		before := time.Now()
		r.Record(timeline.Event{
			Type:    timeline.EventDiscovered,
			Message: "Test message",
		})
		after := time.Now()

		events := r.GetAll()
		require.Len(t, events, 1)

		event := events[0]
		assert.NotEmpty(t, event.ID)
		assert.True(t, event.Timestamp.After(before) || event.Timestamp.Equal(before))
		assert.True(t, event.Timestamp.Before(after) || event.Timestamp.Equal(after))
		assert.Equal(t, timeline.EventDiscovered, event.Type)
		assert.Equal(t, "Test message", event.Message)
	})

	t.Run("preserves provided ID and timestamp", func(t *testing.T) {
		r := timeline.NewRecorder()

		customID := "custom-id"
		customTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		r.Record(timeline.Event{
			ID:        customID,
			Timestamp: customTime,
			Type:      timeline.EventSyncStarted,
			Message:   "Custom event",
		})

		events := r.GetAll()
		require.Len(t, events, 1)

		event := events[0]
		assert.Equal(t, customID, event.ID)
		assert.Equal(t, customTime, event.Timestamp)
	})

	t.Run("returns events newest first", func(t *testing.T) {
		r := timeline.NewRecorder()

		r.Record(timeline.Event{Type: timeline.EventDiscovered, Message: "first"})
		r.Record(timeline.Event{Type: timeline.EventSyncStarted, Message: "second"})
		r.Record(timeline.Event{Type: timeline.EventComplete, Message: "third"})

		events := r.GetAll()
		require.Len(t, events, 3)

		assert.Equal(t, "third", events[0].Message)
		assert.Equal(t, "second", events[1].Message)
		assert.Equal(t, "first", events[2].Message)
	})
}

func TestRecorder_GetByDownload(t *testing.T) {
	r := timeline.NewRecorder()

	r.Record(timeline.Event{DownloadID: "dl-1", Message: "event 1"})
	r.Record(timeline.Event{DownloadID: "dl-2", Message: "event 2"})
	r.Record(timeline.Event{DownloadID: "dl-1", Message: "event 3"})
	r.Record(timeline.Event{DownloadID: "dl-3", Message: "event 4"})

	events := r.GetByDownload("dl-1")
	require.Len(t, events, 2)
	assert.Equal(t, "event 3", events[0].Message)
	assert.Equal(t, "event 1", events[1].Message)
}

func TestRecorder_GetByApp(t *testing.T) {
	r := timeline.NewRecorder()

	r.Record(timeline.Event{AppName: "sonarr", Message: "event 1"})
	r.Record(timeline.Event{AppName: "radarr", Message: "event 2"})
	r.Record(timeline.Event{AppName: "sonarr", Message: "event 3"})

	events := r.GetByApp("sonarr")
	require.Len(t, events, 2)
	assert.Equal(t, "event 3", events[0].Message)
	assert.Equal(t, "event 1", events[1].Message)
}

func TestRecorder_GetByDownloader(t *testing.T) {
	r := timeline.NewRecorder()

	r.Record(timeline.Event{Downloader: "seedbox-1", Message: "event 1"})
	r.Record(timeline.Event{Downloader: "seedbox-2", Message: "event 2"})
	r.Record(timeline.Event{Downloader: "seedbox-1", Message: "event 3"})

	events := r.GetByDownloader("seedbox-1")
	require.Len(t, events, 2)
	assert.Equal(t, "event 3", events[0].Message)
	assert.Equal(t, "event 1", events[1].Message)
}

func TestRecorder_Clear(t *testing.T) {
	r := timeline.NewRecorder()

	r.Record(timeline.Event{DownloadID: "dl-1", Message: "event 1"})
	r.Record(timeline.Event{DownloadID: "dl-2", Message: "event 2"})
	r.Record(timeline.Event{DownloadID: "dl-1", Message: "event 3"})

	r.Clear("dl-1")

	events := r.GetAll()
	require.Len(t, events, 1)
	assert.Equal(t, "dl-2", events[0].DownloadID)
}

func TestRecorder_EventTypes(t *testing.T) {
	// Test that all event types are defined as expected
	types := []timeline.EventType{
		timeline.EventSystemStarted,
		timeline.EventDownloaderConnect,
		timeline.EventAppConnected,
		timeline.EventDiscovered,
		timeline.EventSyncStarted,
		timeline.EventSyncProgress,
		timeline.EventSyncComplete,
		timeline.EventSyncCancelled,
		timeline.EventMovingStarted,
		timeline.EventMoveComplete,
		timeline.EventImportStarted,
		timeline.EventImportComplete,
		timeline.EventImportFailed,
		timeline.EventCategoryChanged,
		timeline.EventRemoved,
		timeline.EventError,
		timeline.EventComplete,
		timeline.EventCleanup,
	}

	for _, et := range types {
		assert.NotEmpty(t, string(et))
	}
}
