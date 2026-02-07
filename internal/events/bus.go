// Package events provides an in-process event bus for decoupled communication.
package events

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Type represents the type of event.
type Type string

// Event types for the sync pipeline.
const (
	// SystemStarted indicates the system has started.
	SystemStarted Type = "system.started"
	// DownloaderConnected indicates a downloader has connected.
	DownloaderConnected Type = "downloader.connected"
	// AppConnected indicates an app has connected.
	AppConnected Type = "app.connected"

	// DownloadDiscovered indicates a new download was discovered.
	DownloadDiscovered Type = "download.discovered"
	// DownloadUpdated indicates download progress/speed changed.
	DownloadUpdated Type = "download.updated"
	// DownloadPaused indicates download was paused in the client.
	DownloadPaused Type = "download.paused"
	// DownloadResumed indicates download was resumed in the client.
	DownloadResumed Type = "download.resumed"
	// DownloadRemoved indicates a download was removed.
	DownloadRemoved Type = "download.removed"
	// CategoryChanged indicates a download's category changed.
	CategoryChanged Type = "category.changed"
	// DownloadComplete indicates all files completed in the downloader.
	DownloadComplete Type = "download.complete"
	// DownloadError indicates a download encountered an error in the client.
	DownloadError Type = "download.error"

	// FileCompleted indicates an individual file completed in the downloader.
	// This enables incremental syncing of multi-file downloads.
	FileCompleted Type = "file.completed"

	// SyncJobCreated indicates a sync job was created in the database.
	SyncJobCreated Type = "sync.job.created"
	// SyncFileCreated indicates a sync file record was created in the database.
	SyncFileCreated Type = "sync.file.created"
	// SyncStarted indicates file sync has started.
	SyncStarted Type = "sync.started"
	// SyncFileStarted indicates a single file has started syncing.
	SyncFileStarted Type = "sync.file.started"
	// SyncFileComplete indicates a single file finished syncing.
	SyncFileComplete Type = "sync.file.complete"
	// SyncComplete indicates all files finished syncing.
	SyncComplete Type = "sync.complete"
	// SyncFailed indicates sync failed with an error.
	SyncFailed Type = "sync.failed"
	// SyncCancelled indicates sync was cancelled.
	SyncCancelled Type = "sync.cancelled"

	// MoveStarted indicates file move has started.
	MoveStarted Type = "move.started"
	// MoveComplete indicates file move completed.
	MoveComplete Type = "move.complete"
	// MoveFailed indicates file move failed with an error.
	MoveFailed Type = "move.failed"

	// AppNotifyStarted indicates app notification has started.
	AppNotifyStarted Type = "app.notify.started"
	// AppNotifyComplete indicates app notification completed successfully.
	AppNotifyComplete Type = "app.notify.complete"
	// AppNotifyFailed indicates app notification failed.
	AppNotifyFailed Type = "app.notify.failed"

	// Deprecated: Use AppNotifyStarted instead.
	ImportStarted Type = "import.started"
	// Deprecated: Use AppNotifyComplete instead.
	ImportComplete Type = "import.complete"
	// Deprecated: Use AppNotifyFailed instead.
	ImportFailed Type = "import.failed"

	// Cleanup indicates cleanup of synced files.
	Cleanup Type = "cleanup"
)

// Event represents an event in the system.
// Subject should be the primary entity the event is about (e.g., *store.Download, *store.App, *store.Downloader).
// Data contains additional event-specific information not available on the Subject.
type Event struct {
	Type      Type           `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Subject   any            `json:"-"` // Primary entity: *store.Download, *store.App, *store.Downloader, or nil
	Data      map[string]any `json:"data,omitempty"`
}

// Subscription is a channel that receives events.
type Subscription <-chan Event

// subscriberEntry tracks a subscriber and its filter.
type subscriberEntry struct {
	ch     chan Event
	types  map[Type]bool // nil means all events
	closed bool
}

// Bus is an in-process event bus that supports pub/sub.
type Bus struct {
	subscribers []*subscriberEntry
	mu          sync.RWMutex
	logger      zerolog.Logger
	bufferSize  int
}

// Option is a functional option for configuring the bus.
type Option func(*Bus)

// WithLogger sets the logger for the bus.
func WithLogger(logger zerolog.Logger) Option {
	return func(b *Bus) {
		b.logger = logger
	}
}

// WithBufferSize sets the channel buffer size for subscribers.
func WithBufferSize(size int) Option {
	return func(b *Bus) {
		b.bufferSize = size
	}
}

// Default buffer size for subscriber channels.
const defaultBufferSize = 100

// New creates a new event bus.
func New(opts ...Option) *Bus {
	b := &Bus{
		logger:     zerolog.Nop(),
		bufferSize: defaultBufferSize,
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

// Subscribe creates a subscription for specific event types.
// If no types are provided, the subscription receives all events.
func (b *Bus) Subscribe(types ...Type) Subscription {
	ch := make(chan Event, b.bufferSize)

	entry := &subscriberEntry{
		ch: ch,
	}

	if len(types) > 0 {
		entry.types = make(map[Type]bool, len(types))
		for _, t := range types {
			entry.types[t] = true
		}
	}

	b.mu.Lock()
	b.subscribers = append(b.subscribers, entry)
	b.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription and closes its channel.
func (b *Bus) Unsubscribe(sub Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, entry := range b.subscribers {
		if entry.ch == sub {
			if !entry.closed {
				close(entry.ch)
				entry.closed = true
			}
			// Remove from slice
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

// Publish sends an event to all matching subscribers.
func (b *Bus) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, entry := range b.subscribers {
		if entry.closed {
			continue
		}

		// Check if subscriber wants this event type
		if entry.types != nil && !entry.types[event.Type] {
			continue
		}

		// Non-blocking send - drop if buffer full
		select {
		case entry.ch <- event:
		default:
			b.logger.Warn().
				Str("type", string(event.Type)).
				Msg("event dropped - subscriber buffer full")
		}
	}

	b.logger.Debug().
		Str("type", string(event.Type)).
		Msg("event published")
}

// Close closes all subscriber channels and cleans up.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, entry := range b.subscribers {
		if !entry.closed {
			close(entry.ch)
			entry.closed = true
		}
	}
	b.subscribers = nil
}

// SubscriberCount returns the number of active subscribers.
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
