//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

func main() {
	opts := []entc.Option{
		entc.FeatureNames(
			"intercept",
			"sql/execquery",
		),
	}

	if err := entc.Generate("./schema", &gen.Config{
		Target:  "./generated",
		Package: "github.com/seedreap/seedreap/internal/ent/generated",
		Features: []gen.Feature{
			gen.FeatureVersionedMigration,
			gen.FeatureUpsert,
		},
	}, opts...); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
