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

// SyncJob holds the schema definition for the SyncJob entity.
// Represents a file synchronization job for transferring files from seedbox.
type SyncJob struct {
	ent.Schema
}

// Mixin of the SyncJob.
func (SyncJob) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the SyncJob.
func (SyncJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_job_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadJob.ID"),
		field.String("remote_base").
			Default(""),
		field.String("local_base").
			Default(""),
		field.Enum("status").
			Values("pending", "syncing", "complete", "cancelled", "error").
			Default("pending"),
		field.String("error_message").
			Default(""),
		field.Time("started_at").
			Optional().
			Nillable().
			Comment("Set when sync actually starts"),
		field.Time("completed_at").
			Optional().
			Nillable(),
		field.Time("cancelled_at").
			Optional().
			Nillable(),
	}
}

// Edges of the SyncJob.
func (SyncJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_job", DownloadJob.Type).
			Ref("sync_job").
			Unique().
			Required().
			Field("download_job_id"),
		edge.To("files", SyncFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the SyncJob.
func (SyncJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_job_id").
			Unique(),
		index.Fields("status"),
	}
}

// Annotations of the SyncJob.
func (SyncJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sync_jobs"},
	}
}
