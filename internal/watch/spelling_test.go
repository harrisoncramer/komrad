package watch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPluralIsTheTableInCamelCase pins how a table's name is turned into the name the panel's
// world holds its rows under.
func TestPluralIsTheTableInCamelCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		table  string
		plural string
	}{
		{table: "pull_requests", plural: "PullRequests"},
		{table: "mail", plural: "Mail"},
		{table: "agents", plural: "Agents"},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.plural, Resource{Table: tt.table}.Plural())
		})
	}
}
