package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/oklog/ulid/v2"

	"github.com/seedreap/seedreap/internal/ent/mixins"
)

// TrackedDownload holds the schema definition for the TrackedDownload entity.
// Represents the high-level view of a download through the workflow pipeline.
type TrackedDownload struct {
	ent.Schema
}

// Mixin of the TrackedDownload.
func (TrackedDownload) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the TrackedDownload.
func (TrackedDownload) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_job_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadJob.ID"),
		field.String("name"),
		field.String("category").
			Default(""),
		field.String("app_name").
			Default("").
			Comment("Current target app (may change with category)"),
		field.Enum("state").
			Values(
				"downloading",
				"downloading_syncing",
				"paused",
				"pending",
				"syncing",
				"synced",
				"move_pending",
				"moving",
				"moved",
				"importing",
				"imported",
				"sync_error",
				"move_error",
				"import_error",
				"error",
				"cancelled",
			).
			Default("downloading"),
		field.String("error_message").
			Default(""),
		field.Int64("total_size").
			Default(0),
		field.Int64("completed_size").
			Default(0).
			Comment("Bytes synced locally"),
		field.Int("total_files").
			Default(0),
		field.Time("discovered_at"),
		field.Time("completed_at").
			Optional().
			Nillable().
			Comment("When fully imported"),
	}
}

// Edges of the TrackedDownload.
func (TrackedDownload) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_job", DownloadJob.Type).
			Ref("tracked_download").
			Unique().
			Required().
			Field("download_job_id"),
	}
}

// Indexes of the TrackedDownload.
func (TrackedDownload) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_job_id").
			Unique(),
		index.Fields("state"),
		index.Fields("discovered_at"),
	}
}

// Annotations of the TrackedDownload.
func (TrackedDownload) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tracked_downloads"},
	}
}
