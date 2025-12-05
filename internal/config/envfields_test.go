//nolint:testpackage // internal test needs access to unexported field lists
package config

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEnvFieldsCoverStructFields verifies that the environment variable field lists
// (downloaderEnvFields and appEnvFields) contain all fields from their respective
// config structs. This test will fail if a new field is added to a struct but not
// added to the corresponding env fields list.
func TestEnvFieldsCoverStructFields(t *testing.T) {
	t.Run("downloaderEnvFields covers DownloaderConfig", func(t *testing.T) {
		expected := extractMapstructureFields(reflect.TypeFor[DownloaderConfig](), "")
		sort.Strings(expected)

		actual := make([]string, len(downloaderEnvFields))
		copy(actual, downloaderEnvFields)
		sort.Strings(actual)

		assert.Equal(t, expected, actual,
			"downloaderEnvFields must contain all fields from DownloaderConfig and SSHConfig.\n"+
				"If you added a new field to DownloaderConfig or SSHConfig, add it to downloaderEnvFields in config.go")
	})

	t.Run("appEnvFields covers AppEntryConfig", func(t *testing.T) {
		expected := extractMapstructureFields(reflect.TypeFor[AppEntryConfig](), "")
		sort.Strings(expected)

		actual := make([]string, len(appEnvFields))
		copy(actual, appEnvFields)
		sort.Strings(actual)

		assert.Equal(t, expected, actual,
			"appEnvFields must contain all fields from AppEntryConfig.\n"+
				"If you added a new field to AppEntryConfig, add it to appEnvFields in config.go")
	})
}

// extractMapstructureFields recursively extracts all mapstructure tag values from a struct type.
// For nested structs, it prefixes the field names with the parent's mapstructure tag (e.g., "ssh.host").
func extractMapstructureFields(t reflect.Type, prefix string) []string {
	var fields []string

	for i := range t.NumField() {
		field := t.Field(i)

		// Get the mapstructure tag value
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}

		fullName := tag
		if prefix != "" {
			fullName = prefix + "." + tag
		}

		// If the field is a struct, recurse into it
		if field.Type.Kind() == reflect.Struct {
			nested := extractMapstructureFields(field.Type, fullName)
			fields = append(fields, nested...)
		} else {
			fields = append(fields, fullName)
		}
	}

	return fields
}
