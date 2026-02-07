// Package mixins provides reusable ent schema mixins for common patterns.
package mixins

import (
	"crypto/rand"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	"github.com/oklog/ulid/v2"
)

// IDMixin provides a ULID-based ID field with auto-generation.
type IDMixin struct {
	mixin.Schema
}

// Fields of the IDMixin.
func (IDMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			GoType(ulid.ULID{}).
			DefaultFunc(func() ulid.ULID {
				// Use crypto/rand.Reader which is thread-safe and cryptographically secure
				return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
			}).
			Unique().
			Immutable(),
	}
}
