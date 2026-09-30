package watch

import (
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheMigratedTriggerFunctionsAreTheDeclaredOnes pins that the schema's trigger functions run the
// bodies declared here.
func TestTheMigratedTriggerFunctionsAreTheDeclaredOnes(t *testing.T) {
	l := newPooled(t)

	names := xslice.Map(TriggerFunctions, func(function TriggerFunction) string { return function.Name })
	rows, err := l.pool.Query(t.Context(), `SELECT proname::text, prosrc FROM pg_proc WHERE proname::text = ANY ($1)`, names)
	require.NoError(t, err)
	defer rows.Close()

	var migrated []TriggerFunction
	for rows.Next() {
		var function TriggerFunction
		require.NoError(t, rows.Scan(&function.Name, &function.Body))
		migrated = append(migrated, function)
	}
	require.NoError(t, rows.Err())

	assert.ElementsMatch(t, TriggerFunctions, migrated, "a trigger function changed without a migration; run mise run db:watch-migration <name>")
}

// TestBuildTriggersWritesKeysIgnoredColumnsAndFeeders pins the arguments each trigger is attached with.
func TestBuildTriggersWritesKeysIgnoredColumnsAndFeeders(t *testing.T) {
	resources := []Resource{
		{
			Table:     "schedules",
			KeyColumn: "id",
		},
		{
			Table:          "repositories",
			KeyColumn:      "url",
			IgnoredColumns: []string{"calls", "updated_at"},
			Feeders: []Feeder{
				{
					Table:  "repository_notes",
					Column: "repository_url",
				},
				{
					Table: "agents",
					Lookup: &Lookup{
						ParentColumn:    "url",
						ChildColumn:     "repository_url",
						SkipParentsWith: "archived_at",
						OnChangeOf:      "branch",
					},
				},
			},
		},
	}

	assert.Equal(t, []Trigger{
		{
			Name:     "watch_notify",
			Table:    "schedules",
			Function: "watch_notify",
			Args:     []string{"id"},
		},
		{
			Name:     "watch_notify",
			Table:    "repositories",
			Function: "watch_notify",
			Args:     []string{"url", "calls", "updated_at"},
		},
		{
			Name:     "watch_subresource_repositories",
			Table:    "repository_notes",
			Function: "watch_subresource",
			Args:     []string{"repositories", "repository_url"},
		},
		{
			Name:     "watch_subresource_repositories",
			Table:    "agents",
			Function: "watch_subresource_lookup",
			Args:     []string{"repositories", "url", "url", "repository_url", "archived_at", "branch"},
		},
	}, BuildTriggers(resources))
}
