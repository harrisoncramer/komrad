package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEmbeddedPostgresCheck covers a down embedded Postgres being told to start rather than reported unreachable.
func TestEmbeddedPostgresCheck(t *testing.T) {
	tests := []struct {
		name       string
		running    bool
		root       bool
		noCluster  bool
		version    string
		want       Status
		wantDetail string
		wantCode   string
		wantFix    string
	}{
		{
			name:       "down, as root",
			root:       true,
			version:    "18.6",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "embedded_postgres_root",
			wantFix:    fixEmbeddedPostgresRoot,
		},
		{
			name:       "down, with no cluster set up",
			noCluster:  true,
			version:    "18.6",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "no_embedded_postgres_cluster",
			wantFix:    fixNoEmbeddedPostgresCluster,
		},
		{
			name:       "down",
			version:    "18.6",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "embedded_postgres_down",
			wantFix:    "komrad start",
		},
		{
			name:       "down, from a build carrying no embedded Postgres",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "no_embedded_postgres",
			wantFix:    fixNoEmbeddedPostgres,
		},
		{
			name:       "running, from a build carrying no embedded Postgres",
			running:    true,
			want:       StatusWarn,
			wantDetail: "/state/postgres",
			wantCode:   "no_embedded_postgres",
		},
		{
			name:       "running",
			running:    true,
			version:    "18.6",
			want:       StatusOK,
			wantDetail: "18.6 in /state/postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Settings:                   Settings{EmbeddedPostgres: true, StateDir: "/state"},
				IsEmbeddedPostgresRunning:  func(context.Context, string) bool { return tt.running },
				HasEmbeddedPostgresCluster: func(string) bool { return !tt.noCluster },
				EmbeddedPostgresVersion:    tt.version,
				IsRoot:                     tt.root,
			}

			result := newEmbeddedPostgresCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			assert.Equal(t, tt.wantFix, result.Fix)
		})
	}
}
