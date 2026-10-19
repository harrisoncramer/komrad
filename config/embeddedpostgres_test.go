package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsEmbeddedPostgresURL covers which database urls reach the embedded Postgres in a directory.
func TestIsEmbeddedPostgresURL(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		want        bool
	}{
		{
			name:        "the url komrad derives",
			databaseURL: BuildEmbeddedPostgresURL("/state/postgres", defaultEngineLogin, EmbeddedPostgresDatabase),
			want:        true,
		},
		{
			name:        "the owner on the same socket, spelled with a trailing slash",
			databaseURL: "postgres://komrad@/komrad?host=/state/postgres/",
			want:        true,
		},
		{
			name:        "a socket in another directory",
			databaseURL: "postgres://komrad@/komrad?host=/var/run/postgresql",
		},
		{
			name:        "a server over TCP",
			databaseURL: "postgres://komrad@localhost:5432/komrad",
		},
		{
			name:        "a url that does not parse",
			databaseURL: "postgres://komrad@%zz/komrad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEmbeddedPostgresURL(tt.databaseURL, "/state/postgres"))
		})
	}
}
