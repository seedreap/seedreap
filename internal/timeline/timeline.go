// Package timeline provides event tracking throughout the sync pipeline.
package timeline

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// EventType represents the type of timeline event.
type EventType string

// Event types for the timeline.
const (
	EventSystemStarted     EventType = "system_started"
	EventDownloaderConnect EventType = "downloader_connected"
	EventAppConnected      EventType = "app_connected"
	EventDiscovered        EventType = "discovered"
	EventSyncStarted       EventType = "sync_started"
	EventSyncProgress      EventType = "sync_progress"
	EventSyncComplete      EventType = "sync_complete"
	EventSyncCancelled     EventType = "sync_cancelled"
	EventMovingStarted     EventType = "moving_started"
	EventMoveComplete      EventType = "move_complete"
	EventImportStarted     EventType = "import_started"
	EventImportComplete    EventType = "import_complete"
	EventImportFailed      EventType = "import_failed"
	EventCategoryChanged   EventType = "category_changed"
	EventRemoved           EventType = "removed"
	EventError             EventType = "error"
	EventComplete          EventType = "complete"
	EventCleanup           EventType = "cleanup"
)

// Event represents a single timeline event.
type Event struct {
	ID           string         `json:"id"`
	Type         EventType      `json:"type"`
	Timestamp    time.Time      `json:"timestamp"`
	Message      string         `json:"message"`
	DownloadID   string         `json:"download_id,omitempty"`
	DownloadName string         `json:"download_name,omitempty"`
	AppName      string         `json:"app_name,omitempty"`
	Downloader   string         `json:"downloader,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// Recorder records and retrieves timeline events.
type Recorder interface {
	// Record adds a new event to the timeline.
	Record(event Event)

	// GetAll returns all events, newest first.
	GetAll() []Event

	// GetByDownload returns events for a specific download, newest first.
	GetByDownload(downloadID string) []Event

	// GetByApp returns events for a specific app, newest first.
	GetByApp(appName string) []Event

	// GetByDownloader returns events for a specific downloader, newest first.
	GetByDownloader(downloaderName string) []Event

	// Clear removes all events for a download.
	Clear(downloadID string)
}

// recorder is the default in-memory implementation of Recorder.
type recorder struct {
	events    []Event
	mu        sync.RWMutex
	logger    zerolog.Logger
	maxEvents int
	nextID    int64
}

// Option is a functional option for configuring the recorder.
type Option func(*recorder)

// WithLogger sets the logger.
func WithLogger(logger zerolog.Logger) Option {
	return func(r *recorder) {
		r.logger = logger
	}
}

// WithMaxEvents sets the maximum number of events to retain.
func WithMaxEvents(maxEvents int) Option {
	return func(r *recorder) {
		r.maxEvents = maxEvents
	}
}

// Default configuration values.
const (
	defaultMaxEvents = 10000
)

// NewRecorder creates a new timeline recorder.
func NewRecorder(opts ...Option) Recorder {
	r := &recorder{
		events:    make([]Event, 0),
		logger:    zerolog.Nop(),
		maxEvents: defaultMaxEvents,
		nextID:    1,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Record adds a new event to the timeline.
func (r *recorder) Record(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Generate ID if not set
	if event.ID == "" {
		event.ID = r.generateID()
	}

	// Set timestamp if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Prepend event (newest first)
	r.events = append([]Event{event}, r.events...)

	// Trim if over max
	if len(r.events) > r.maxEvents {
		r.events = r.events[:r.maxEvents]
	}

	r.logger.Debug().
		Str("id", event.ID).
		Str("type", string(event.Type)).
		Str("message", event.Message).
		Msg("timeline event recorded")
}

// GetAll returns all events, newest first.
func (r *recorder) GetAll() []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy
	result := make([]Event, len(r.events))
	copy(result, r.events)
	return result
}

// GetByDownload returns events for a specific download, newest first.
func (r *recorder) GetByDownload(downloadID string) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Event
	for _, e := range r.events {
		if e.DownloadID == downloadID {
			result = append(result, e)
		}
	}
	return result
}

// GetByApp returns events for a specific app, newest first.
func (r *recorder) GetByApp(appName string) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Event
	for _, e := range r.events {
		if e.AppName == appName {
			result = append(result, e)
		}
	}
	return result
}

// GetByDownloader returns events for a specific downloader, newest first.
func (r *recorder) GetByDownloader(downloaderName string) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Event
	for _, e := range r.events {
		if e.Downloader == downloaderName {
			result = append(result, e)
		}
	}
	return result
}

// Clear removes all events for a download.
func (r *recorder) Clear(downloadID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var filtered []Event
	for _, e := range r.events {
		if e.DownloadID != downloadID {
			filtered = append(filtered, e)
		}
	}
	r.events = filtered
}

// generateID generates a unique event ID.
func (r *recorder) generateID() string {
	id := r.nextID
	r.nextID++
	return "evt_" + time.Now().Format("20060102150405") + "_" + itoa(id)
}

// itoa converts an int64 to string without importing strconv.
func itoa(i int64) string {
	if i == 0 {
		return "0"
	}

	var b [20]byte
	pos := len(b)
	neg := i < 0
	if neg {
		i = -i
	}

	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}

	if neg {
		pos--
		b[pos] = '-'
	}

	return string(b[pos:])
}
