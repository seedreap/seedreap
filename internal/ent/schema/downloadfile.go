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

// DownloadFile holds the schema definition for the DownloadFile entity.
// Represents a file within a download job.
type DownloadFile struct {
	ent.Schema
}

// Mixin of the DownloadFile.
func (DownloadFile) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the DownloadFile.
func (DownloadFile) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_job_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadJob.ID"),
		field.String("relative_path"),
		field.Int64("size").
			Default(0),
		field.Int64("downloaded").
			Default(0).
			Comment("Bytes downloaded so far"),
		field.Float("progress").
			Default(0).
			Comment("File progress 0.0-1.0"),
		field.Int("priority").
			Default(0).
			Comment("Download priority from client"),
	}
}

// Edges of the DownloadFile.
func (DownloadFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_job", DownloadJob.Type).
			Ref("files").
			Unique().
			Required().
			Field("download_job_id"),
		edge.To("sync_file", SyncFile.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the DownloadFile.
func (DownloadFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_job_id"),
	}
}

// Annotations of the DownloadFile.
func (DownloadFile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "download_files"},
	}
}
