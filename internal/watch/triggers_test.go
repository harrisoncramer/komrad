package watch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
