package store

import (
	"context"
	"testing"

	"weave/db/testmodels"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineIsolationHoldsForEveryTable fails for a table or key added later that one engine could reach across.
func TestEngineIsolationHoldsForEveryTable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	queries := testmodels.New(open(ctx, t).GetRawPool())

	tests := []struct {
		name  string
		list  func(context.Context) ([]string, error)
		cause string
	}{
		{"tenant", queries.ListTablesWithoutTenant, "these tables have no required tenant_id"},
		{"isolation", queries.ListTablesWithoutEngineIsolation, "these tables lack row security, the engine_isolation policy or the stamp_tenant trigger"},
		{"foreign keys", queries.ListForeignKeysWithoutTenant, "these foreign keys can point at another engine's row"},
		{"set null", queries.ListSetNullKeysClearingTenant, "these foreign keys clear tenant_id on delete; name the column in ON DELETE SET NULL (...)"},
		{"unique keys", queries.ListUniqueKeysAcrossEngines, "these unique keys collide across engines; lead them with tenant_id"},
		{"grants", queries.ListTablesEnginesCannotReach, "weave_engine or weave_reader cannot reach these tables"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offending, err := tt.list(ctx)
			require.NoError(t, err)
			assert.Empty(t, offending, tt.cause)
		})
	}
}
