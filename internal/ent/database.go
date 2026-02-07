package ent

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	// SQLite driver.
	_ "modernc.org/sqlite"

	"github.com/seedreap/seedreap/internal/ent/generated"

	// Import runtime to register schema hooks and interceptors.
	_ "github.com/seedreap/seedreap/internal/ent/generated/runtime"
)

// Option is a functional option for configuring the database client.
type Option func(*options)

type options struct {
	debug bool
}

// WithDebug enables debug logging for SQL queries.
func WithDebug() Option {
	return func(o *options) {
		o.debug = true
	}
}

// Open opens a database connection and returns an Ent client.
// The dialect should be one of: "sqlite3", "postgres", "mysql".
// For SQLite, the DSN should be a path to the database file or ":memory:" for in-memory.
func Open(driverDialect, dsn string, opts ...Option) (*generated.Client, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// Map common driver names to Ent dialects
	entDialect := mapDialect(driverDialect)

	// Open the database connection
	db, err := sql.Open(driverDialect, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Apply SQLite-specific configuration
	if entDialect == dialect.SQLite {
		if err = configureSQLite(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	// Create Ent driver from sql.DB
	drv := entsql.OpenDB(entDialect, db)

	// Build client options
	var clientOpts []generated.Option
	clientOpts = append(clientOpts, generated.Driver(drv))
	if o.debug {
		clientOpts = append(clientOpts, generated.Debug())
	}

	return generated.NewClient(clientOpts...), nil
}

// OpenSQLite is a convenience function for opening a SQLite database.
func OpenSQLite(dsn string, opts ...Option) (*generated.Client, error) {
	return Open("sqlite", dsn, opts...)
}

// mapDialect maps driver names to Ent dialect constants.
func mapDialect(driver string) string {
	switch driver {
	case "sqlite", "sqlite3":
		return dialect.SQLite
	case "postgres", "postgresql", "pgx":
		return dialect.Postgres
	case "mysql":
		return dialect.MySQL
	default:
		return driver
	}
}

// configureSQLite applies SQLite-specific PRAGMA settings.
func configureSQLite(db *sql.DB) error {
	ctx := context.Background()

	// Enable foreign keys
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	return nil
}

// Migrate runs auto-migrations on the database schema.
// This creates or updates tables to match the current Ent schema.
func Migrate(ctx context.Context, client *generated.Client) error {
	if err := client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}
