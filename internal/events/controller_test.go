package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/event"
	"github.com/seedreap/seedreap/internal/events"
	testpkg "github.com/seedreap/seedreap/internal/testing"
)

func TestEventsController(t *testing.T) {
	t.Run("records all events", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()
		db := testpkg.NewTestDB(t)

		c := events.NewController(bus, db, events.WithControllerLogger(zerolog.Nop()))
		err := c.Start(context.Background())
		require.NoError(t, err)
		defer c.Stop()

		// Create a test download job to use as Subject
		testDownload := &generated.DownloadJob{
			RemoteID:         "hash123",
			Name:             "Test.Download",
			DownloadClientID: ulid.Make(),
		}

		// Publish various events
		bus.Publish(events.Event{
			Type: events.SystemStarted,
		})

		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: testDownload,
			Data: map[string]any{
				"category": "tv",
			},
		})

		bus.Publish(events.Event{
			Type:    events.SyncStarted,
			Subject: testDownload,
		})

		time.Sleep(100 * time.Millisecond)

		// Verify events were recorded
		timeline, err := db.Event.Query().
			Order(event.ByTimestamp()).
			All(context.Background())
		require.NoError(t, err)
		assert.Len(t, timeline, 3)
	})

	t.Run("generates correct messages", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()
		db := testpkg.NewTestDB(t)

		c := events.NewController(bus, db, events.WithControllerLogger(zerolog.Nop()))
		err := c.Start(context.Background())
		require.NoError(t, err)
		defer c.Stop()

		testDownload := &generated.DownloadJob{
			RemoteID:         "hash123",
			Name:             "Test.Download",
			DownloadClientID: ulid.Make(),
		}

		bus.Publish(events.Event{
			Type:    events.DownloadDiscovered,
			Subject: testDownload,
		})

		time.Sleep(50 * time.Millisecond)

		timeline, err := db.Event.Query().
			Order(event.ByTimestamp()).
			Limit(1).
			All(context.Background())
		require.NoError(t, err)
		require.Len(t, timeline, 1)
		assert.Equal(t, "Discovered: Test.Download", timeline[0].Message)
	})

	t.Run("handles category changed event", func(t *testing.T) {
		bus := events.New()
		defer bus.Close()
		db := testpkg.NewTestDB(t)

		c := events.NewController(bus, db, events.WithControllerLogger(zerolog.Nop()))
		err := c.Start(context.Background())
		require.NoError(t, err)
		defer c.Stop()

		testDownload := &generated.DownloadJob{
			RemoteID:         "hash123",
			Name:             "Test.Download",
			DownloadClientID: ulid.Make(),
		}

		bus.Publish(events.Event{
			Type:    events.CategoryChanged,
			Subject: testDownload,
			Data: map[string]any{
				"old_category": "tv",
				"new_category": "movies",
			},
		})

		time.Sleep(50 * time.Millisecond)

		timeline, err := db.Event.Query().
			Order(event.ByTimestamp()).
			Limit(1).
			All(context.Background())
		require.NoError(t, err)
		require.Len(t, timeline, 1)
		assert.Contains(t, timeline[0].Message, "Category changed")
		assert.Contains(t, timeline[0].Message, "tv -> movies")
	})
}
