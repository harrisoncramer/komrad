package agents

import (
	"testing"

	"komrad/internal/ui/tabs"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// TestEveryStripIsBuiltFromItsTable covers each screen's tabs being its column table and its
// fallback one of them, so escape lands on a column the rows can be ordered by.
func TestEveryStripIsBuiltFromItsTable(t *testing.T) {
	tests := []struct {
		name  string
		strip tabs.Strip
		table []tabs.Option
	}{
		{
			name:  "the agents tree",
			strip: agentsTabs(),
			table: tabs.OptionsOf(agentSorts),
		},
		{
			name:  "the archived agents",
			strip: archivedAgentTabs(),
			table: tabs.OptionsOf(archivedAgentSorts),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.table, tt.strip.Options)

			ids := xslice.Map(tt.strip.Options, func(option tabs.Option) string { return option.ID })
			assert.Contains(t, ids, tt.strip.Fallback)
		})
	}
}
