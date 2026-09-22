package watch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSpellings pins how a table and a message name are turned into the names the checks
// look for.
func TestSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		table  string
		snaked string
		camel  string
	}{
		{name: "PullRequest", table: "pull_requests", snaked: "pull_request", camel: "PullRequests"},
		{name: "Mail", table: "mail", snaked: "mail", camel: "Mail"},
		{name: "Agent", table: "agents", snaked: "agent", camel: "Agents"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.snaked, snake(tt.name))
			assert.Equal(t, tt.camel, camel(tt.table))
		})
	}
}
