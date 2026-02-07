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

// DownloadJob holds the schema definition for the DownloadJob entity.
// Represents a tracked download from a download client (seedbox).
type DownloadJob struct {
	ent.Schema
}

// Mixin of the DownloadJob.
func (DownloadJob) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields of the DownloadJob.
func (DownloadJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("download_client_id").
			GoType(ulid.ULID{}).
			Comment("References DownloadClient.ID"),
		field.String("remote_id").
			Comment("ID/hash from the download client"),
		field.String("name").
			Comment("Display name of the download"),
		field.String("category").
			Default(""),
		field.String("previous_category").
			Default(""),
		field.Enum("status").
			Values("downloading", "paused", "complete", "error").
			Default("downloading").
			Comment("State in download client"),
		field.Int64("size").
			Default(0).
			Comment("Total size in bytes"),
		field.Int64("downloaded").
			Default(0).
			Comment("Bytes downloaded so far"),
		field.Float("progress").
			Default(0).
			Comment("Download progress 0.0-1.0"),
		field.Int64("download_speed").
			Default(0).
			Comment("Current download speed bytes/sec"),
		field.String("save_path").
			Default(""),
		field.String("content_path").
			Default(""),
		field.String("error_message").
			Default(""),
		field.Time("discovered_at").
			Comment("When the download was first discovered"),
		field.Time("downloaded_at").
			Optional().
			Nillable().
			Comment("When download completed on seedbox"),
	}
}

// Edges of the DownloadJob.
func (DownloadJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("download_client", DownloadClient.Type).
			Ref("download_jobs").
			Unique().
			Required().
			Field("download_client_id"),
		edge.To("tracked_download", TrackedDownload.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("files", DownloadFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("sync_job", SyncJob.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("move_job", MoveJob.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("app_jobs", AppJob.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the DownloadJob.
func (DownloadJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("download_client_id", "remote_id").
			Unique(),
		index.Fields("status"),
		index.Fields("discovered_at"),
	}
}

// Annotations of the DownloadJob.
func (DownloadJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "download_jobs"},
	}
}
