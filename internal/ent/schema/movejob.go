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

// MoveJob holds the schema definition for the MoveJob entity.
// Represents a job to move files from staging to final location.
type MoveJob struct {
	ent.Schema
}

// Mixin of the MoveJob.
func (MoveJob) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the MoveJob.
func (MoveJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_job_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadJob.ID"),
		field.String("source_path").
			Default(""),
		field.String("destination_path").
			Default(""),
		field.Enum("status").
			Values("pending", "moving", "complete", "error").
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

// Edges of the MoveJob.
func (MoveJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_job", DownloadJob.Type).
			Ref("move_job").
			Unique().
			Required().
			Field("download_job_id"),
	}
}

// Indexes of the MoveJob.
func (MoveJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_job_id").
			Unique(),
		index.Fields("status"),
	}
}

// Annotations of the MoveJob.
func (MoveJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "move_jobs"},
	}
}
