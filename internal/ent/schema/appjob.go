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

// AppJob holds the schema definition for the AppJob entity.
// Represents a job to notify an app about downloaded files.
type AppJob struct {
	ent.Schema
}

// Mixin of the AppJob.
func (AppJob) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the AppJob.
func (AppJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_job_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadJob.ID"),
		field.String("app_name"),
		field.String("path").
			Default("").
			Comment("Path sent to app for import"),
		field.Enum("status").
			Values("pending", "processing", "complete", "error").
			Default("pending"),
		field.String("error_message").
			Default(""),
		field.Time("started_at").
			Optional().
			Nillable(),
		field.Time("completed_at").
			Optional().
			Nillable(),
	}
}

// Edges of the AppJob.
func (AppJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_job", DownloadJob.Type).
			Ref("app_jobs").
			Unique().
			Required().
			Field("download_job_id"),
	}
}

// Indexes of the AppJob.
func (AppJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_job_id"),
		index.Fields("app_name"),
		index.Fields("status"),
		index.Fields("download_job_id", "app_name").
			Unique(),
	}
}

// Annotations of the AppJob.
func (AppJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "app_jobs"},
	}
}
