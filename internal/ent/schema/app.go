// Package schema defines ent schemas for database entities.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/ent/mixins"
)

// App holds the schema definition for the App entity.
// Represents a configured application (Sonarr, Radarr, etc.).
type App struct {
	ent.Schema
}

// Mixin of the App.
func (App) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
	}
}

// Fields of the App.
func (App) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Unique(),
		field.Enum("type").
			Values("sonarr", "radarr", "passthrough").
			Comment("App type"),
		field.String("url").
			Default(""),
		field.String("api_key").
			Default("").
			Sensitive(),
		field.String("category").
			Default("").
			Comment("Download category this app handles"),
		field.String("downloads_path").
			Default("").
			Comment("Path where downloads are placed for this app"),
		field.Int64("http_timeout").
			Default(int64(config.DefaultHTTPTimeout)),
		field.Bool("cleanup_on_category_change").
			Default(false).
			Comment("Delete synced files when category changes"),
		field.Bool("cleanup_on_remove").
			Default(false).
			Comment("Delete synced files when download is removed"),
		field.Bool("enabled").
			Default(true),
		field.Time("last_connected").
			Optional().
			Nillable(),
		field.String("last_error").
			Default(""),
	}
}

// Indexes of the App.
func (App) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("category"),
		index.Fields("enabled"),
	}
}

// Annotations of the App.
func (App) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "apps"},
	}
}
