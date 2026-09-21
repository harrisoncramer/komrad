package schedule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduleWritesTouchTheSchedule pins that a create, an update and an archive each name
// the schedule.
func TestScheduleWritesTouchTheSchedule(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	flow := seedWorkflow(t, st)
	st.touched.Reset()

	created, err := st.Create(ctx, Config{Name: "nightly", Cron: "0 0 * * *", Timezone: "UTC", Prompt: "go", WorkflowID: flow})
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, st.touched.Schedules(), "a create names the new schedule")

	st.touched.Reset()
	require.NoError(t, st.SetEnabled(ctx, created.ID, false))
	assert.Equal(t, []string{created.ID}, st.touched.Schedules())

	st.touched.Reset()
	require.NoError(t, st.Archive(ctx, created.ID))
	assert.Equal(t, []string{created.ID}, st.touched.Schedules())
}
