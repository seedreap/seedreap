package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/event"
)

// Controller persists events to the database for history tracking.
// It follows a microservice pattern: communicating only via the database and event bus,
// with no direct dependencies on other domain packages.
//
// The Controller is responsible for:
// - Subscribing to all events on the bus
// - Persisting events to the timeline in the database
// - Generating human-readable messages for events.
type Controller struct {
	eventBus *Bus
	db       *generated.Client
	logger   zerolog.Logger

	subscription Subscription
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// ControllerOption is a functional option for configuring the Controller.
type ControllerOption func(*Controller)

// WithControllerLogger sets the logger for the controller.
func WithControllerLogger(logger zerolog.Logger) ControllerOption {
	return func(c *Controller) {
		c.logger = logger
	}
}

// NewController creates a new events Controller.
func NewController(eventBus *Bus, db *generated.Client, opts ...ControllerOption) *Controller {
	c := &Controller{
		eventBus: eventBus,
		db:       db,
		logger:   zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start begins processing all events for persistence.
func (c *Controller) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)

	// Subscribe to all events (no filter)
	c.subscription = c.eventBus.Subscribe()

	c.wg.Add(1)
	go c.run(ctx)

	c.logger.Info().Msg("events controller started")
	return nil
}

// Stop stops the controller and waits for it to finish.
func (c *Controller) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	c.eventBus.Unsubscribe(c.subscription)
	c.wg.Wait()

	c.logger.Info().Msg("events controller stopped")
	return nil
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.subscription:
			if !ok {
				return
			}
			c.recordEvent(ctx, event)
		}
	}
}

func (c *Controller) recordEvent(ctx context.Context, ev Event) {
	// Generate a human-readable message
	message := c.generateMessage(ev)

	var timestamp time.Time
	if ev.Timestamp.IsZero() {
		timestamp = time.Now()
	} else {
		timestamp = ev.Timestamp
	}

	// Serialize event data to JSON for details field
	var details string
	if len(ev.Data) > 0 {
		if jsonBytes, err := json.Marshal(ev.Data); err == nil {
			details = string(jsonBytes)
		}
	}

	// Extract subject type and ID directly from Subject
	subjectType, subjectID := extractSubject(ev.Subject)

	// Get app name from Data if present (for app-related events)
	appName, _ := ev.Data["app_name"].(string)

	// Build the event creation query
	create := c.db.Event.Create().
		SetType(string(ev.Type)).
		SetMessage(message).
		SetSubjectType(subjectType).
		SetAppName(appName).
		SetDetails(details).
		SetTimestamp(timestamp).
		SetCreatedAt(time.Now())

	// Set subject ID if present
	if subjectID != nil {
		create.SetSubjectID(subjectID.String())
	}

	if _, err := create.Save(ctx); err != nil {
		c.logger.Error().Err(err).
			Str("event_type", string(ev.Type)).
			Msg("failed to record event")
		return
	}

	c.logger.Debug().
		Str("event_type", string(ev.Type)).
		Str("subject_type", string(subjectType)).
		Msg("recorded event")
}

// extractSubject extracts the subject type and ID from an event's Subject field.
func extractSubject(subject any) (event.SubjectType, *ulid.ULID) {
	if subject == nil {
		return event.SubjectTypeSystem, nil
	}

	switch s := subject.(type) {
	case *generated.DownloadJob:
		if s != nil {
			return event.SubjectTypeDownload, &s.ID
		}
	case *generated.App:
		if s != nil {
			return event.SubjectTypeApp, &s.ID
		}
	case *generated.DownloadClient:
		if s != nil {
			return event.SubjectTypeDownloader, &s.ID
		}
	}

	return event.SubjectTypeSystem, nil
}

// getSubjectName extracts a name from the event subject.
func getSubjectName(subject any) string {
	switch s := subject.(type) {
	case *generated.DownloadJob:
		if s != nil {
			return s.Name
		}
	case *generated.App:
		if s != nil {
			return s.Name
		}
	case *generated.DownloadClient:
		if s != nil {
			return s.Name
		}
	}
	return ""
}

//nolint:funlen // switch statement for message generation is intentionally long
func (c *Controller) generateMessage(event Event) string {
	name := getSubjectName(event.Subject)
	appName, _ := event.Data["app_name"].(string)

	switch event.Type {
	case SystemStarted:
		return "System started"
	case DownloaderConnected:
		return fmt.Sprintf("Connected to downloader: %s", name)
	case AppConnected:
		return fmt.Sprintf("Connected to app: %s", name)
	case DownloadDiscovered:
		return fmt.Sprintf("Discovered: %s", name)
	case DownloadUpdated:
		return fmt.Sprintf("Updated: %s", name)
	case DownloadPaused:
		return fmt.Sprintf("Paused: %s", name)
	case DownloadResumed:
		return fmt.Sprintf("Resumed: %s", name)
	case DownloadRemoved:
		return fmt.Sprintf("Removed: %s", name)
	case CategoryChanged:
		oldCat, _ := event.Data["old_category"].(string)
		newCat, _ := event.Data["new_category"].(string)
		return fmt.Sprintf("Category changed: %s (%s -> %s)", name, oldCat, newCat)
	case DownloadComplete:
		return fmt.Sprintf("Download complete on seedbox: %s", name)
	case DownloadError:
		return fmt.Sprintf("Download error: %s", name)
	case FileCompleted:
		fileName, _ := event.Data["file_path"].(string)
		return fmt.Sprintf("File complete: %s - %s", name, fileName)
	case SyncJobCreated:
		return fmt.Sprintf("Sync job created: %s", name)
	case SyncFileCreated:
		fileName, _ := event.Data["file_path"].(string)
		return fmt.Sprintf("Sync file queued: %s - %s", name, fileName)
	case SyncStarted:
		return fmt.Sprintf("Sync started: %s", name)
	case SyncFileStarted:
		fileName, _ := event.Data["file_path"].(string)
		return fmt.Sprintf("File sync started: %s - %s", name, fileName)
	case SyncFileComplete:
		fileName, _ := event.Data["file_path"].(string)
		return fmt.Sprintf("File synced: %s - %s", name, fileName)
	case SyncComplete:
		return fmt.Sprintf("Sync complete: %s", name)
	case SyncFailed:
		return fmt.Sprintf("Sync failed: %s", name)
	case SyncCancelled:
		return fmt.Sprintf("Sync cancelled: %s", name)
	case MoveStarted:
		return fmt.Sprintf("Move started: %s", name)
	case MoveComplete:
		return fmt.Sprintf("Move complete: %s", name)
	case MoveFailed:
		return fmt.Sprintf("Move failed: %s", name)
	case AppNotifyStarted:
		return fmt.Sprintf("App notify started: %s -> %s", name, appName)
	case AppNotifyComplete:
		return fmt.Sprintf("App notify complete: %s -> %s", name, appName)
	case AppNotifyFailed:
		return fmt.Sprintf("App notify failed: %s -> %s", name, appName)
	case Cleanup:
		return fmt.Sprintf("Cleanup: %s", name)
	default:
		return fmt.Sprintf("Event: %s", event.Type)
	}
}
