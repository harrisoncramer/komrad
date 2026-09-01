package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCountLiveForSchedule pins that work still owed counts against a schedule while an
// idle agent does not, since nothing will move it and the schedule would block for good.
func TestCountLiveForSchedule(t *testing.T) {
	tests := []struct {
		name          string
		states        []string
		archive       bool
		lookElsewhere bool
		want          int64
	}{
		{
			name: "a schedule that has produced nothing has nothing running",
		},
		{
			name:   "an agent still thinking counts",
			states: []string{StateThinking},
			want:   1,
		},
		{
			name:   "so does one waiting on a prompt, which is unfinished work",
			states: []string{StateWaiting},
			want:   1,
		},
		{
			name:   "an idle one does not",
			states: []string{StateIdle},
			want:   0,
		},
		{
			name:   "several unfinished agents are all counted",
			states: []string{StateThinking, StateWaiting, StateIdle},
			want:   2,
		},
		{
			name:    "an archived agent is not running, whatever state it stopped in",
			states:  []string{StateThinking},
			archive: true,
			want:    0,
		},
		{
			name:          "another schedule's agents are not this one's",
			states:        []string{StateThinking},
			lookElsewhere: true,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			schedule := seedSchedule(t, st)
			for i, state := range tt.states {
				name := fmt.Sprintf("agent-%d", i)
				require.NoError(t, st.Create(ctx, Agent{
					Name:       name,
					State:      state,
					ScheduleID: &schedule,
				}))
				if tt.archive {
					require.NoError(t, st.archiveByName(ctx, name))
				}
			}

			target := schedule
			if tt.lookElsewhere {
				target = seedSchedule(t, st)
			}

			count, err := st.CountLiveForSchedule(ctx, target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count)
		})
	}
}

// TestCountLive covers the count a create is measured against, which every row the panel
// holds contributes to whatever its state and whether or not it is shown.
func TestCountLive(t *testing.T) {
	tests := []struct {
		name    string
		states  []string
		hide    bool
		archive bool
		want    int64
	}{
		{
			name: "a panel holding nothing has room",
		},
		{
			name:   "an agent still thinking counts",
			states: []string{StateThinking},
			want:   1,
		},
		{
			name:   "so does an idle one, which is still a row to be dealt with",
			states: []string{StateIdle},
			want:   1,
		},
		{
			name:   "every state at once",
			states: []string{StateThinking, StateWaiting, StateIdle},
			want:   3,
		},
		{
			name:   "a hidden agent counts, since hiding it stopped nothing",
			states: []string{StateThinking},
			hide:   true,
			want:   1,
		},
		{
			name:    "an archived agent does not, which is how the count comes down",
			states:  []string{StateThinking, StateIdle},
			archive: true,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for i, state := range tt.states {
				name := fmt.Sprintf("agent-%d", i)
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: state}))

				written, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, written)

				if tt.hide {
					hidden, err := st.SetHidden(ctx, written.ID, true)
					require.NoError(t, err)
					require.True(t, hidden)
				}
				if tt.archive {
					_, archiveErr := st.Archive(ctx, []string{written.ID})
					require.NoError(t, archiveErr)
				}
			}

			count, err := st.CountLive(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count)
		})
	}
}
