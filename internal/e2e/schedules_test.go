//go:build integration

package e2e

import (
	"testing"
	"time"

	"komrad/internal/harness"
	"komrad/internal/komradtest"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	viewv1 "komrad/proto/gen/view/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationSchedulesLaunchOnTheirFirings walks one schedule from its creation through its firings to its archive.
func TestIntegrationSchedulesLaunchOnTheirFirings(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflowID := createWorkflow(t, instance, "nightly")
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	var (
		scheduleID string
		launched   *agentv1.Agent
	)

	t.Run("a new schedule reports when it next fires", func(t *testing.T) {
		created, err := instance.Schedules.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
			Name:       "morning-ci",
			Cron:       "0 9 * * *",
			Prompt:     "check CI",
			Enabled:    true,
			WorkflowId: workflowID,
		}))
		require.NoError(t, err)

		scheduleID = created.Msg.GetSchedule().GetId()
		assert.NotNil(t, created.Msg.GetSchedule().GetNextRunAt())
		assert.Empty(t, listScheduleRuns(t, instance, scheduleID), "nothing has fired yet")
	})

	t.Run("a firing launches the schedule's agent and records the run", func(t *testing.T) {
		instance.Fire(scheduleID, nine)

		agents := listScheduleAgents(t, instance, scheduleID)
		require.Len(t, agents, 1)
		launched = agents[0]
		assert.Equal(t, "morning-ci", launched.GetName())
		assert.Equal(t, "check CI", launched.GetPrompt())
		assert.Equal(t, workflowID, launched.GetWorkflowId())
		instance.WaitForHarness(launched.GetId())

		runs := listScheduleRuns(t, instance, scheduleID)
		require.Len(t, runs, 1)
		assert.Equal(t, schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED, runs[0].GetState())
		assert.Equal(t, nine, runs[0].GetScheduledFor().AsTime())
	})

	t.Run("a firing while that agent is working launches nothing", func(t *testing.T) {
		instance.Settle(launched.GetId(), harness.ActivityWorking, agentstore.StateThinking)

		instance.Fire(scheduleID, nine.Add(24*time.Hour))

		assert.Len(t, listScheduleAgents(t, instance, scheduleID), 1)
		runs := listScheduleRuns(t, instance, scheduleID)
		require.Len(t, runs, 2)
		assert.Equal(t, schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED, runs[0].GetState())
		assert.Contains(t, runs[0].GetMessage(), "still has 1 agent(s) running")

		instance.Settle(launched.GetId(), harness.ActivityIdle, agentstore.StateIdle)
	})

	t.Run("a paused schedule launches nothing", func(t *testing.T) {
		_, err := instance.Schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
			Id: scheduleID,
		}))
		require.NoError(t, err)

		paused, err := instance.Schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: scheduleID}))
		require.NoError(t, err)
		assert.Nil(t, paused.Msg.GetSchedule().GetNextRunAt(), "a paused schedule has no next run")

		instance.Fire(scheduleID, nine.Add(48*time.Hour))

		assert.Len(t, listScheduleAgents(t, instance, scheduleID), 1)
		runs := listScheduleRuns(t, instance, scheduleID)
		require.Len(t, runs, 3)
		assert.Equal(t, schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED, runs[0].GetState())
		assert.Contains(t, runs[0].GetMessage(), "is paused")
	})

	t.Run("an archived schedule is gone and its agent stays", func(t *testing.T) {
		_, err := instance.Schedules.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: scheduleID}))
		require.NoError(t, err)

		_, err = instance.Schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: scheduleID}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

		listed, err := instance.Schedules.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{}))
		require.NoError(t, err)
		assert.Empty(t, listed.Msg.GetSchedules())

		assert.Len(t, listScheduleAgents(t, instance, scheduleID), 1, "the agent it launched is left alone")
	})
}

func listScheduleAgents(t *testing.T, instance *komradtest.Instance, scheduleID string) []*agentv1.Agent {
	t.Helper()

	listed, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)

	agents := xslice.Map(listed.Msg.GetAgents(), func(row *agentv1.AgentRow) *agentv1.Agent {
		return row.GetAgent()
	})

	return xslice.Filter(agents, func(agent *agentv1.Agent) bool {
		return agent.GetScheduleId() == scheduleID
	})
}

func listScheduleRuns(t *testing.T, instance *komradtest.Instance, scheduleID string) []*schedulev1.ScheduleRun {
	t.Helper()

	listed, err := instance.Schedules.ListScheduleRuns(t.Context(), connect.NewRequest(&schedulev1.ListScheduleRunsRequest{Id: scheduleID}))
	require.NoError(t, err)

	return listed.Msg.GetRuns()
}
