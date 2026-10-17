package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPostgresCheck covers a down Postgres being told to start rather than reported unreachable.
func TestPostgresCheck(t *testing.T) {
	tests := []struct {
		name       string
		running    bool
		noCluster  bool
		version    string
		want       Status
		wantDetail string
		wantCode   string
		wantFix    string
	}{
		{
			name:       "down, with no cluster set up",
			noCluster:  true,
			version:    "18.6.0",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "postgres_no_cluster",
			wantFix:    fixNoPostgresCluster,
		},
		{
			name:       "down",
			version:    "18.6.0",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "postgres_down",
			wantFix:    "komrad start",
		},
		{
			name:       "down, from a build carrying no Postgres",
			want:       StatusFail,
			wantDetail: "/state/postgres",
			wantCode:   "postgres_not_bundled",
			wantFix:    fixPostgresNotBundled,
		},
		{
			name:       "running, from a build carrying no Postgres",
			running:    true,
			want:       StatusWarn,
			wantDetail: "/state/postgres",
			wantCode:   "postgres_not_bundled",
		},
		{
			name:       "running",
			running:    true,
			version:    "18.6.0",
			want:       StatusOK,
			wantDetail: "18.6.0 in /state/postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Settings:           Settings{RunPostgres: true, StateDir: "/state"},
				IsPostgresRunning:  func(context.Context, string) bool { return tt.running },
				HasPostgresCluster: func(string) bool { return !tt.noCluster },
				PostgresVersion:    tt.version,
			}

			result := newPostgresCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			assert.Equal(t, tt.wantFix, result.Fix)
		})
	}
}
