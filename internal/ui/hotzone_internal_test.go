package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestHotZoneHeat covers which agents are hot, for what reason and since when.
func TestHotZoneHeat(t *testing.T) {
	changed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	earlier := changed.Add(-time.Hour)
	live := func(state panel.State) panel.AgentRow {
		return panel.AgentRow{
			Agent: panel.Agent{ID: "a1", State: state, StateChangedAt: changed},
			Alive: true,
		}
	}

	tests := []struct {
		name string
		was  map[string]hotEntry
		item panel.AgentRow
		keep string
		want string
		// since is when the agent became hot, unchecked when it is not.
		since time.Time
	}{
		{
			name: "an agent at work is not hot",
			item: live(panel.StateThinking),
			want: "",
		},
		{
			name: "a prompt in a pane that has gone is not hot",
			item: panel.AgentRow{Agent: panel.Agent{ID: "a1", State: panel.StateWaiting}},
			want: "",
		},
		{
			name: "a paused agent is not hot",
			item: func() panel.AgentRow {
				item := live(panel.StateWaiting)
				item.Agent.HeldAt = xptr.Ptr(changed)

				return item
			}(),
			want: "",
		},
		{
			name: "a hidden agent is not hot",
			item: func() panel.AgentRow {
				item := live(panel.StateWaiting)
				item.Agent.Hidden = true

				return item
			}(),
			want: "",
		},
		{
			name: "a conflict an agent is working through is not hot",
			item: func() panel.AgentRow {
				item := live(panel.StateThinking)
				item.Agent.SequenceInProgress = true

				return item
			}(),
			want: "",
		},
		{
			name: "an agent komrad cannot read and was not hot stays out",
			item: live(panel.StateUncertain),
			want: "",
		},
		{
			name:  "an agent komrad cannot read stays in",
			was:   map[string]hotEntry{"a1": {since: earlier, rule: hotRules[0]}},
			item:  live(panel.StateUncertain),
			want:  "asking",
			since: earlier,
		},
		{
			name:  "the row kept stays in once it is answered",
			was:   map[string]hotEntry{"a1": {since: earlier, rule: hotRules[0]}},
			item:  live(panel.StateThinking),
			keep:  agentRowKey("a1"),
			want:  "asking",
			since: earlier,
		},
		{
			name:  "an agent that stays hot keeps when it became hot",
			was:   map[string]hotEntry{"a1": {since: earlier, rule: hotRules[0]}},
			item:  live(panel.StateErrored),
			want:  "errored",
			since: earlier,
		},
		{
			name:  "a prompt is asking",
			item:  live(panel.StateWaiting),
			want:  "asking",
			since: changed,
		},
		{
			name:  "a failed turn has errored",
			item:  panel.AgentRow{Agent: panel.Agent{ID: "a1", State: panel.StateErrored, StateChangedAt: changed}},
			want:  "errored",
			since: changed,
		},
		{
			name:  "a launch that failed",
			item:  live(panel.StateFailedToCreate),
			want:  "launch failed",
			since: changed,
		},
		{
			name:  "a cleanup that failed",
			item:  live(panel.StateFailedToCleanup),
			want:  "cleanup failed",
			since: changed,
		},
		{
			name: "a script that failed",
			item: func() panel.AgentRow {
				item := live(panel.StateIdle)
				item.FailedRun = true

				return item
			}(),
			want:  "script failed",
			since: changed,
		},
		{
			name: "a conflict left for a person",
			item: func() panel.AgentRow {
				item := live(panel.StateIdle)
				item.Agent.SequenceInProgress = true

				return item
			}(),
			want:  "conflict",
			since: changed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone := hotZone{capacity: 1, entries: tt.was}

			zone.heat([]panel.AgentRow{tt.item}, tt.keep)

			entry, hot := zone.entries["a1"]
			assert.Equal(t, tt.want, entry.rule.reason)
			assert.Equal(t, tt.want != "", hot)
			if hot {
				assert.Equal(t, tt.since, entry.since)
			}
		})
	}
}
