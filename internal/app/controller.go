package app

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/app"
	"github.com/seedreap/seedreap/internal/ent/generated/appjob"
	"github.com/seedreap/seedreap/internal/events"
)

// Default configuration values for the controller.
const defaultAppHTTPTimeout = 30 * time.Second

// Controller handles app notifications based on events.
// It follows a microservice pattern: communicating only via the database and event bus,
// with no direct dependencies on other domain packages.
//
// The Controller is responsible for:
// - Listening for MoveComplete events
// - Looking up apps by category from the database
// - Triggering app notifications/imports
// - Emitting app.notify.* events for status tracking.
type Controller struct {
	eventBus *events.Bus
	db       *generated.Client
	logger   zerolog.Logger

	subscription events.Subscription
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

// NewController creates a new app Controller.
func NewController(
	eventBus *events.Bus,
	db *generated.Client,
	opts ...ControllerOption,
) *Controller {
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

// Start begins processing events.
func (c *Controller) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)

	// Subscribe to events we care about
	c.subscription = c.eventBus.Subscribe(
		events.MoveComplete,
	)

	c.wg.Add(1)
	go c.run(ctx)

	c.logger.Info().Msg("app controller started")
	return nil
}

// Stop stops the controller and waits for it to finish.
func (c *Controller) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	if c.subscription != nil {
		c.eventBus.Unsubscribe(c.subscription)
	}
	c.logger.Info().Msg("app controller stopped")
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
			c.handleEvent(ctx, event)
		}
	}
}

func (c *Controller) handleEvent(ctx context.Context, event events.Event) {
	switch event.Type {
	case events.MoveComplete:
		c.handleMoveComplete(ctx, event)
	default:
		// Ignore other event types
	}
}

func (c *Controller) handleMoveComplete(ctx context.Context, event events.Event) {
	// Get the download job from the event subject
	dlJob, ok := event.Subject.(*generated.DownloadJob)
	if !ok || dlJob == nil {
		c.logger.Error().Msg("event subject is not a download job")
		return
	}

	c.logger.Info().
		Str("download_hash", dlJob.RemoteID).
		Str("download_name", dlJob.Name).
		Msg("received move.complete event, will notify apps")

	// Get apps for this category from database
	apps, err := c.db.App.Query().
		Where(app.CategoryEQ(dlJob.Category)).
		All(ctx)
	if err != nil {
		c.logger.Error().Err(err).
			Str("category", dlJob.Category).
			Msg("failed to get apps for category")
		return
	}

	if len(apps) == 0 {
		c.logger.Warn().
			Str("download", dlJob.Name).
			Str("category", dlJob.Category).
			Msg("no apps configured for category, skipping notification")
		return
	}

	// Get final path from event data
	finalPath, _ := event.Data["final_path"].(string)
	if finalPath == "" {
		c.logger.Error().
			Str("download_hash", dlJob.RemoteID).
			Msg("no final path in move complete event")
		return
	}

	// The final path already includes the download name from filesync controller
	importPath := finalPath

	// Notify each app
	for _, appCfg := range apps {
		c.wg.Add(1)
		go func(cfg *generated.App) {
			defer c.wg.Done()
			c.notifyApp(ctx, dlJob, cfg, importPath)
		}(appCfg)
	}
}

func (c *Controller) notifyApp(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	appCfg *generated.App,
	importPath string,
) {
	appName := appCfg.Name

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("app", appName).
		Str("path", importPath).
		Msg("notifying app")

	// Create AppJob record to track the notification
	aj, createErr := c.db.AppJob.Create().
		SetDownloadJobID(dlJob.ID).
		SetAppName(appName).
		SetPath(importPath).
		SetStatus(appjob.StatusPending).
		Save(ctx)
	if createErr != nil {
		c.logger.Error().Err(createErr).
			Str("download", dlJob.Name).
			Str("app", appName).
			Msg("failed to create app job")
		return
	}

	// Update app job status to processing
	now := time.Now()
	_, updateErr := aj.Update().
		SetStatus(appjob.StatusProcessing).
		SetStartedAt(now).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("app_job_id", aj.ID.String()).
			Msg("failed to update app job status")
	}
	aj.Status = appjob.StatusProcessing
	aj.StartedAt = &now

	// Publish app notify started event
	c.eventBus.Publish(events.Event{
		Type:    events.AppNotifyStarted,
		Subject: dlJob,
		Data: map[string]any{
			"app_name":    appName,
			"app_job_id":  aj.ID.String(),
			"import_path": importPath,
		},
	})

	// Build the app client from database configuration
	appClient := c.buildAppClient(appCfg)
	if appClient == nil {
		errMsg := "unknown app type: " + string(appCfg.Type)
		c.logger.Error().
			Str("app", appName).
			Str("type", string(appCfg.Type)).
			Msg("failed to build app client")
		c.handleNotifyError(ctx, dlJob, aj, appName, errMsg)
		return
	}

	// Trigger the import
	if err := appClient.TriggerImport(ctx, importPath); err != nil {
		c.logger.Error().Err(err).
			Str("download", dlJob.Name).
			Str("app", appName).
			Msg("app notification failed")
		c.handleNotifyError(ctx, dlJob, aj, appName, err.Error())
		return
	}

	c.logger.Info().
		Str("download", dlJob.Name).
		Str("app", appName).
		Msg("app notification complete")

	// Update app job status to complete
	completedNow := time.Now()
	_, completeErr := aj.Update().
		SetStatus(appjob.StatusComplete).
		SetCompletedAt(completedNow).
		Save(ctx)
	if completeErr != nil {
		c.logger.Error().Err(completeErr).
			Str("app_job_id", aj.ID.String()).
			Msg("failed to update app job to complete")
	}

	// Publish app notify complete event
	c.eventBus.Publish(events.Event{
		Type:    events.AppNotifyComplete,
		Subject: dlJob,
		Data: map[string]any{
			"app_name":    appName,
			"app_job_id":  aj.ID.String(),
			"import_path": importPath,
		},
	})
}

func (c *Controller) handleNotifyError(
	ctx context.Context,
	dlJob *generated.DownloadJob,
	aj *generated.AppJob,
	appName string,
	errMsg string,
) {
	// Update app job status to error
	_, updateErr := aj.Update().
		SetStatus(appjob.StatusError).
		SetErrorMessage(errMsg).
		Save(ctx)
	if updateErr != nil {
		c.logger.Error().Err(updateErr).
			Str("app_job_id", aj.ID.String()).
			Msg("failed to update app job error status")
	}

	c.eventBus.Publish(events.Event{
		Type:    events.AppNotifyFailed,
		Subject: dlJob,
		Data: map[string]any{
			"app_name":   appName,
			"app_job_id": aj.ID.String(),
			"error":      errMsg,
		},
	})
}

func (c *Controller) buildAppClient(appCfg *generated.App) App {
	httpTimeout := time.Duration(appCfg.HTTPTimeout) * time.Second
	if httpTimeout == 0 {
		httpTimeout = defaultAppHTTPTimeout
	}

	switch appCfg.Type {
	case app.TypeSonarr:
		return NewSonarr(
			appCfg.Name,
			ArrConfig{
				URL:           appCfg.URL,
				APIKey:        appCfg.APIKey,
				Category:      appCfg.Category,
				DownloadsPath: appCfg.DownloadsPath,
				HTTPTimeout:   httpTimeout,
			},
			WithLogger(c.logger.With().Str("app", appCfg.Name).Logger()),
		)

	case app.TypeRadarr:
		return NewRadarr(
			appCfg.Name,
			ArrConfig{
				URL:           appCfg.URL,
				APIKey:        appCfg.APIKey,
				Category:      appCfg.Category,
				DownloadsPath: appCfg.DownloadsPath,
				HTTPTimeout:   httpTimeout,
			},
			WithLogger(c.logger.With().Str("app", appCfg.Name).Logger()),
		)

	case app.TypePassthrough:
		return NewPassthrough(
			appCfg.Name,
			appCfg.Category,
			appCfg.DownloadsPath,
			WithLogger(c.logger.With().Str("app", appCfg.Name).Logger()),
		)

	default:
		return nil
	}
}
