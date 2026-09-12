package db

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaVersionChangesWithTheMigrations is what a template database built from an older
// schema is told apart by, so anything the migrations do differently has to move it.
func TestSchemaVersionChangesWithTheMigrations(t *testing.T) {
	base := fstest.MapFS{
		"20260101000000_first.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
	}

	version, err := schemaVersion(base)
	require.NoError(t, err)
	require.Len(t, version, versionLength)

	moved := map[string]fs.FS{
		"a migration was edited": fstest.MapFS{
			"20260101000000_first.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
		},
		"a migration was renamed": fstest.MapFS{
			"20260101000000_second.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
		},
		"a migration was added": fstest.MapFS{
			"20260101000000_first.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
			"20260102000000_second.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
		},
	}

	for name, files := range moved {
		t.Run(name, func(t *testing.T) {
			other, err := schemaVersion(files)
			require.NoError(t, err)
			assert.NotEqual(t, version, other)
		})
	}

	again, err := schemaVersion(base)
	require.NoError(t, err)
	assert.Equal(t, version, again, "the same migrations fingerprint the same twice")
}

// TestSchemaVersionReadsTheEmbeddedMigrations covers the exported call a test database is
// named after.
func TestSchemaVersionReadsTheEmbeddedMigrations(t *testing.T) {
	version, err := SchemaVersion()
	require.NoError(t, err)

	assert.Len(t, version, versionLength)
}
