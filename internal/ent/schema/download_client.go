package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/seedreap/seedreap/internal/config"
	"github.com/seedreap/seedreap/internal/ent/mixins"
)

// DownloadClient holds the schema definition for the DownloadClient entity.
// Represents a configured download client (qBittorrent, etc.).
type DownloadClient struct {
	ent.Schema
}

// Mixin of the DownloadClient.
func (DownloadClient) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.IDMixin{},
		mixins.TimestampMixin{},
	}
}

// Fields of the DownloadClient.
func (DownloadClient) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Unique(),
		field.String("type").
			Comment("Downloader type (qbittorrent, etc.)"),
		field.String("url"),
		field.String("username").
			Default(""),
		field.String("password").
			Default("").
			Sensitive(),
		field.Int64("http_timeout").
			Default(int64(config.DefaultHTTPTimeout)),
		field.Bool("enabled").
			Default(true),
		field.String("ssh_host").
			Default(""),
		field.Int("ssh_port").
			Default(config.DefaultSSHPort),
		field.String("ssh_user").
			Default(""),
		field.String("ssh_key_file").
			Default(""),
		field.String("ssh_known_hosts_file").
			Default(""),
		field.Bool("ssh_ignore_host_key").
			Default(false),
		field.Int64("ssh_timeout").
			Default(int64(config.DefaultSSHTimeout)),
		field.Time("last_connected").
			Optional().
			Nillable(),
		field.String("last_error").
			Default(""),
	}
}

// Edges of the DownloadClient.
func (DownloadClient) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("download_jobs", DownloadJob.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the DownloadClient.
func (DownloadClient) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled"),
	}
}

// Annotations of the DownloadClient.
func (DownloadClient) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "download_clients"},
	}
}
