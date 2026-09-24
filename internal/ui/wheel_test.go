package ui

import (
	"testing"
	"time"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScrollKeysMoveTheListARow pins the keys the mouse wheel presses moving the cursor of every
// list one row, the way the wheel moves a selection.
func TestScrollKeysMoveTheListARow(t *testing.T) {
	tests := []struct {
		name   string
		model  func(t *testing.T) model
		cursor func(m model) int
	}{
		{
			name: "agents",
			model: func(*testing.T) model {
				m := screensModel()
				m.screen = screenAgents
				m.agents.cursor = 0

				return m
			},
			cursor: func(m model) int { return m.agents.cursor },
		},
		{
			name: "archive",
			model: func(t *testing.T) model {
				m := splitArchiveModel(t)
				m.archive.list().jump(&m, m.shared(), 0)

				return m
			},
			cursor: func(m model) int { return m.archive.cursor },
		},
		{
			name: "schedules",
			model: func(*testing.T) model {
				m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
				m.listing.schedules = scheduleItems("a", "b", "c")
				m.schedules.cursor = 0

				return m
			},
			cursor: func(m model) int { return m.schedules.cursor },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.model(t)
			press := func(pressed string) {
				next, _ := m.route(keyMsgOf(t, pressed))
				m = next.(model)
			}

			require.Equal(t, 0, tt.cursor(m))

			press("ctrl+n")
			assert.Equal(t, 1, tt.cursor(m), "one turn down is one row")

			press("ctrl+p")
			assert.Equal(t, 0, tt.cursor(m), "and one turn up is back")

			press("ctrl+p")
			assert.Equal(t, 0, tt.cursor(m), "the top stops it")
		})
	}
}

// TestScrollKeysStopAtTheFocusedRecordsTop pins a turn up on the first field keeping the record.
func TestScrollKeysStopAtTheFocusedRecordsTop(t *testing.T) {
	m := screensModel()
	m.height = 30
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "", inspection: readWorktree(time.Now())})

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.isDetailsFocused())
	require.Equal(t, 0, focused.detailsCursor(focused.hoveredDetails()))

	down := send(t, focused, "ctrl+n")
	assert.Equal(t, 1, down.detailsCursor(down.hoveredDetails()), "a turn down is a field")

	up := send(t, down, "ctrl+p")
	assert.True(t, up.isDetailsFocused(), "a turn up onto the first field keeps the record")
	assert.Equal(t, 0, up.detailsCursor(up.hoveredDetails()))

	past := send(t, up, "ctrl+p")
	assert.True(t, past.isDetailsFocused(), "and a turn up past it keeps the record too")
	assert.Equal(t, 0, past.detailsCursor(past.hoveredDetails()))
}
