package mixins

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/seedreap/seedreap/internal/ent/generated"
	"github.com/seedreap/seedreap/internal/ent/generated/hook"
	"github.com/seedreap/seedreap/internal/ent/generated/intercept"
)

// SkipSoftDelete returns a context that includes soft-deleted records in queries
// and performs hard deletes on delete operations.
func SkipSoftDelete(ctx context.Context) context.Context {
	return context.WithValue(ctx, softDeleteSkipKey{}, true)
}

// softDeleteKey is the context key for skipping soft delete filtering.
type softDeleteSkipKey struct{}

// SoftDeleteMixin implements the soft delete pattern for schemas.
type SoftDeleteMixin struct {
	mixin.Schema
}

// Fields of the SoftDeleteMixin.
func (SoftDeleteMixin) Fields() []ent.Field {
	return []ent.Field{
		field.Time("deleted_at").
			Optional().
			Nillable(),
	}
}

// Interceptors of the SoftDeleteMixin.
func (d SoftDeleteMixin) Interceptors() []ent.Interceptor {
	return []ent.Interceptor{
		intercept.TraverseFunc(func(ctx context.Context, q intercept.Query) error {
			// Skip soft-delete, means include soft-deleted entities.
			if skip, _ := ctx.Value(softDeleteSkipKey{}).(bool); skip {
				return nil
			}

			d.P(q)
			return nil
		}),
	}
}

// SoftDeleteHook will soft delete records, by changing the delete mutation to an update and setting
// the deleted_at field, unless the softDeleteSkipKey is set.
func (d SoftDeleteMixin) SoftDeleteHook(next ent.Mutator) ent.Mutator {
	return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
		// Skip soft-delete, means delete the entity permanently.
		if skip, _ := ctx.Value(softDeleteSkipKey{}).(bool); skip {
			return next.Mutate(ctx, m)
		}

		mx, ok := m.(interface {
			SetOp(ent.Op)
			Client() *generated.Client
			SetDeletedAt(time.Time)
			WhereP(...func(*sql.Selector))
		})
		if !ok {
			return nil, fmt.Errorf("unexpected mutation type %T", m)
		}
		d.P(mx)
		mx.SetOp(ent.OpUpdate)
		mx.SetDeletedAt(time.Now())
		return mx.Client().Mutate(ctx, m)
	})
}

// Hooks of the SoftDeleteMixin.
func (d SoftDeleteMixin) Hooks() []ent.Hook {
	return []ent.Hook{
		hook.On(
			d.SoftDeleteHook,
			ent.OpDeleteOne|ent.OpDelete,
		),
	}
}

// P adds a storage-level predicate to queries.
func (d SoftDeleteMixin) P(w interface{ WhereP(...func(*sql.Selector)) }) {
	w.WhereP(
		sql.FieldIsNull(d.Fields()[0].Descriptor().Name),
	)
}
