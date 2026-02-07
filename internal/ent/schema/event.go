package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/seedreap/seedreap/internal/ent/mixins"
)

// Event holds the schema definition for the Event entity.
// Represents an event in the system timeline.
type Event struct {
	ent.Schema
}

// Mixin of the Event.
func (Event) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
	}
}

// Fields of the Event.
func (Event) Fields() []ent.Field {
	return []ent.Field{
		field.String("type"),
		field.String("message").
			Default(""),
		field.Enum("subject_type").
			Values("system", "download", "app", "downloader", "sync_job", "move_job", "app_job").
			Default("system").
			Comment("Type of entity this event is about"),
		field.String("subject_id").
			Optional().
			Nillable().
			Comment("ID of the entity (nullable for system events)"),
		field.String("app_name").
			Default("").
			Comment("Secondary reference to app"),
		field.String("details").
			Default("").
			Comment("JSON-encoded extra data"),
		field.Time("timestamp"),
		field.Time("created_at"),
	}
}

// Indexes of the Event.
func (Event) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type"),
		index.Fields("subject_type", "subject_id"),
		index.Fields("app_name"),
		index.Fields("timestamp"),
	}
}

// Annotations of the Event.
func (Event) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "events"},
	}
}
