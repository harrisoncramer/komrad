package doctor

import (
	"context"
	"slices"
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// TestPostgresCheck covers a down Postgres being told to start rather than reported unreachable.
func TestPostgresCheck(t *testing.T) {
	tests := []struct {
		name       string
		running    bool
		version    string
		want       Status
		wantDetail string
		wantCode   string
		wantFix    string
	}{
		{
			name:       "running",
			running:    true,
			version:    "18.6.0",
			want:       StatusOK,
			wantDetail: "18.6.0 in /state/postgres",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Settings:          Settings{RunPostgres: true, StateDir: "/state"},
				IsPostgresRunning: func(context.Context, string) bool { return tt.running },
				PostgresVersion:   tt.version,
			}

			result := newPostgresCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			assert.Equal(t, tt.wantFix, result.Fix)
		})
	}
}

// TestDatabaseCheckWaitsOnPostgres covers a down Postgres reporting once, as the thing to start,
// and only when komrad runs it.
func TestDatabaseCheckWaitsOnPostgres(t *testing.T) {
	tests := []struct {
		name        string
		runPostgres bool
		wantNeeds   []string
	}{
		{name: "komrad runs Postgres", runPostgres: true, wantNeeds: []string{"postgres"}},
		{name: "a Postgres of the person's", runPostgres: false, wantNeeds: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := EngineChecks(Settings{RunPostgres: tt.runPostgres})
			names := xslice.Map(checks, func(check Check) string { return check.Name })

			assert.Equal(t, tt.runPostgres, slices.Contains(names, "postgres"))
			for _, check := range checks {
				if check.Name == "database" {
					assert.Equal(t, tt.wantNeeds, check.Needs)
				}
			}
		})
	}
}
