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

// SyncFile holds the schema definition for the SyncFile entity.
// Represents an individual file within a sync job.
type SyncFile struct {
	ent.Schema
}

// Mixin of the SyncFile.
func (SyncFile) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the SyncFile.
func (SyncFile) Fields() []ent.Field {
	return []ent.Field{
		field.String("sync_job_id").
			GoType(ulid.ULID{}).
			Comment("References SyncJob.ID"),
		field.String("download_file_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadFile.ID"),
		field.String("relative_path"),
		field.Int64("size").
			Default(0),
		field.Int64("synced_size").
			Default(0),
		field.Enum("status").
			Values("pending", "syncing", "complete", "error", "cancelled").
			Default("pending"),
		field.String("error_message").
			Default(""),
	}
}

// Edges of the SyncFile.
func (SyncFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("sync_job", SyncJob.Type).
			Ref("files").
			Unique().
			Required().
			Field("sync_job_id"),
		edge.From("download_file", DownloadFile.Type).
			Ref("sync_file").
			Unique().
			Required().
			Field("download_file_id"),
	}
}

// Indexes of the SyncFile.
func (SyncFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("sync_job_id"),
		index.Fields("download_file_id"),
	}
}

// Annotations of the SyncFile.
func (SyncFile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sync_files"},
	}
}
