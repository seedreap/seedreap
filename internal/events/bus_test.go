package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/events"
)

func TestNew(t *testing.T) {
	t.Run("creates bus with defaults", func(t *testing.T) {
		bus := events.New()
		require.NotNil(t, bus)
		assert.Equal(t, 0, bus.SubscriberCount())
	})

	t.Run("applies options", func(t *testing.T) {
		logger := zerolog.Nop()
		bus := events.New(
			events.WithLogger(logger),
			events.WithBufferSize(50),
		)
		require.NotNil(t, bus)
		// Verify buffer size by behavior: we can publish 50 events without blocking
		sub := bus.Subscribe()
		for range 50 {
			bus.Publish(events.Event{Type: events.SyncStarted})
		}
		// Should be able to receive all 50
		for range 50 {
			select {
			case <-sub:
				// Good
			case <-time.After(100 * time.Millisecond):
				t.Fatal("expected to receive all 50 events")
			}
		}
		bus.Unsubscribe(sub)
	})
}

func TestSubscribe(t *testing.T) {
	t.Run("subscribes to all events", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		assert.Equal(t, 1, bus.SubscriberCount())
		assert.NotNil(t, sub)

		bus.Unsubscribe(sub)
		assert.Equal(t, 0, bus.SubscriberCount())
	})

	t.Run("subscribes to specific event types", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe(events.SyncStarted, events.SyncComplete)

		assert.Equal(t, 1, bus.SubscriberCount())

		// Publish matching event
		bus.Publish(events.Event{Type: events.SyncStarted, Data: map[string]any{"test_id": "test-1"}})

		select {
		case event := <-sub:
			assert.Equal(t, events.SyncStarted, event.Type)
			assert.Equal(t, "test-1", event.Data["test_id"])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected to receive event")
		}

		// Publish non-matching event
		bus.Publish(events.Event{Type: events.DownloadDiscovered, Data: map[string]any{"test_id": "test-2"}})

		select {
		case <-sub:
			t.Fatal("should not receive non-matching event")
		case <-time.After(50 * time.Millisecond):
			// Expected - no event received
		}

		bus.Unsubscribe(sub)
	})

	t.Run("multiple subscribers receive same event", func(t *testing.T) {
		bus := events.New()
		sub1 := bus.Subscribe()
		sub2 := bus.Subscribe()

		assert.Equal(t, 2, bus.SubscriberCount())

		bus.Publish(events.Event{Type: events.SyncStarted, Data: map[string]any{"test_id": "test-1"}})

		// Both should receive
		for _, sub := range []events.Subscription{sub1, sub2} {
			select {
			case event := <-sub:
				assert.Equal(t, events.SyncStarted, event.Type)
			case <-time.After(100 * time.Millisecond):
				t.Fatal("expected to receive event")
			}
		}

		bus.Unsubscribe(sub1)
		bus.Unsubscribe(sub2)
	})
}

func TestPublish(t *testing.T) {
	t.Run("sets timestamp if not provided", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		before := time.Now()
		bus.Publish(events.Event{Type: events.SyncStarted})
		after := time.Now()

		event := <-sub
		assert.False(t, event.Timestamp.IsZero())
		assert.True(t, event.Timestamp.After(before) || event.Timestamp.Equal(before))
		assert.True(t, event.Timestamp.Before(after) || event.Timestamp.Equal(after))

		bus.Unsubscribe(sub)
	})

	t.Run("preserves provided timestamp", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		customTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		bus.Publish(events.Event{Type: events.SyncStarted, Timestamp: customTime})

		event := <-sub
		assert.Equal(t, customTime, event.Timestamp)

		bus.Unsubscribe(sub)
	})

	t.Run("drops event when buffer full", func(t *testing.T) {
		bus := events.New(events.WithBufferSize(2))
		sub := bus.Subscribe()

		// Fill the buffer
		bus.Publish(events.Event{Type: events.SyncStarted, Data: map[string]any{"id": "1"}})
		bus.Publish(events.Event{Type: events.SyncStarted, Data: map[string]any{"id": "2"}})

		// This should be dropped (non-blocking)
		bus.Publish(events.Event{Type: events.SyncStarted, Data: map[string]any{"id": "3"}})

		// Drain and verify only 2 events
		var received []string
		for range 3 {
			select {
			case event := <-sub:
				id, _ := event.Data["id"].(string)
				received = append(received, id)
			case <-time.After(50 * time.Millisecond):
				// No more events
			}
		}

		assert.Equal(t, []string{"1", "2"}, received)

		bus.Unsubscribe(sub)
	})

	t.Run("skips closed subscribers", func(_ *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		bus.Unsubscribe(sub)

		// Should not panic
		bus.Publish(events.Event{Type: events.SyncStarted})
	})
}

func TestUnsubscribe(t *testing.T) {
	t.Run("closes channel", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		bus.Unsubscribe(sub)

		// Channel should be closed
		_, ok := <-sub
		assert.False(t, ok)
	})

	t.Run("removes from subscribers list", func(t *testing.T) {
		bus := events.New()
		sub1 := bus.Subscribe()
		sub2 := bus.Subscribe()
		sub3 := bus.Subscribe()

		assert.Equal(t, 3, bus.SubscriberCount())

		bus.Unsubscribe(sub2)
		assert.Equal(t, 2, bus.SubscriberCount())

		// sub1 and sub3 should still work
		bus.Publish(events.Event{Type: events.SyncStarted})

		select {
		case <-sub1:
			// Good
		case <-time.After(100 * time.Millisecond):
			t.Fatal("sub1 should receive event")
		}

		select {
		case <-sub3:
			// Good
		case <-time.After(100 * time.Millisecond):
			t.Fatal("sub3 should receive event")
		}

		bus.Unsubscribe(sub1)
		bus.Unsubscribe(sub3)
	})

	t.Run("handles non-existent subscription", func(_ *testing.T) {
		bus := events.New()
		fakeChan := make(chan events.Event)

		// Should not panic
		bus.Unsubscribe(fakeChan)
	})

	t.Run("handles double unsubscribe", func(_ *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		bus.Unsubscribe(sub)
		// Should not panic
		bus.Unsubscribe(sub)
	})
}

func TestClose(t *testing.T) {
	t.Run("closes all subscriber channels", func(t *testing.T) {
		bus := events.New()
		sub1 := bus.Subscribe()
		sub2 := bus.Subscribe()
		sub3 := bus.Subscribe()

		bus.Close()

		// All channels should be closed
		for _, sub := range []events.Subscription{sub1, sub2, sub3} {
			_, ok := <-sub
			assert.False(t, ok)
		}

		assert.Equal(t, 0, bus.SubscriberCount())
	})

	t.Run("handles already closed subscribers", func(_ *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		bus.Unsubscribe(sub)

		// Should not panic when closing already-closed subscriber
		bus.Close()
	})
}

func TestConcurrency(t *testing.T) {
	t.Run("concurrent subscribe and publish", func(_ *testing.T) {
		bus := events.New(events.WithBufferSize(1000))
		var wg sync.WaitGroup

		// Start multiple publishers
		for range 10 {
			wg.Go(func() {
				for range 100 {
					bus.Publish(events.Event{
						Type: events.SyncStarted,
						Data: map[string]any{"test_id": "test"},
					})
				}
			})
		}

		// Start multiple subscribers
		subs := make([]events.Subscription, 5)
		for i := range 5 {
			subs[i] = bus.Subscribe()
		}

		// Let publishers run
		wg.Wait()

		// Cleanup
		for _, sub := range subs {
			bus.Unsubscribe(sub)
		}
	})

	t.Run("concurrent subscribe and unsubscribe", func(t *testing.T) {
		bus := events.New()
		var wg sync.WaitGroup

		for range 100 {
			wg.Go(func() {
				sub := bus.Subscribe()
				time.Sleep(time.Millisecond)
				bus.Unsubscribe(sub)
			})
		}

		wg.Wait()
		assert.Equal(t, 0, bus.SubscriberCount())
	})
}

func TestEventTypes(t *testing.T) {
	t.Run("all event types are distinct", func(t *testing.T) {
		types := []events.Type{
			events.SystemStarted,
			events.DownloaderConnected,
			events.AppConnected,
			events.DownloadDiscovered,
			events.DownloadRemoved,
			events.CategoryChanged,
			events.DownloadComplete,
			events.DownloadError,
			events.SyncStarted,
			events.SyncFileComplete,
			events.SyncComplete,
			events.SyncCancelled,
			events.MoveStarted,
			events.MoveComplete,
			events.ImportStarted,
			events.ImportComplete,
			events.ImportFailed,
			events.Cleanup,
		}

		seen := make(map[events.Type]bool)
		for _, et := range types {
			assert.False(t, seen[et], "duplicate event type: %s", et)
			seen[et] = true
		}
	})
}

func TestEventData(t *testing.T) {
	t.Run("preserves all event fields", func(t *testing.T) {
		bus := events.New()
		sub := bus.Subscribe()

		// Using a simple subject for testing
		testSubject := "test-subject"

		original := events.Event{
			Type:    events.SyncComplete,
			Subject: testSubject,
			Data: map[string]any{
				"download_id":   "dl-123",
				"download_name": "My Download",
				"app_name":      "sonarr",
				"files_synced":  5,
				"total_size":    int64(1024000),
			},
		}

		bus.Publish(original)

		received := <-sub
		assert.Equal(t, original.Type, received.Type)
		assert.Equal(t, testSubject, received.Subject)
		assert.Equal(t, original.Data["download_id"], received.Data["download_id"])
		assert.Equal(t, original.Data["download_name"], received.Data["download_name"])
		assert.Equal(t, original.Data["app_name"], received.Data["app_name"])
		assert.Equal(t, original.Data["files_synced"], received.Data["files_synced"])
		assert.Equal(t, original.Data["total_size"], received.Data["total_size"])

		bus.Unsubscribe(sub)
	})
}
