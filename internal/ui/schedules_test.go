package ui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	agentv1 "komrad/proto/gen/agent/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	schedulesScreen = "the schedules"
	scheduleForm    = "minute hour day month weekday"
	archiveAsk      = `Archive schedule "morning-ci"?`
)

// TestSchedulesKeys covers what each key does on the schedule under the cursor.
func TestSchedulesKeys(t *testing.T) {
	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		wantScreen string
		wantDrawn  []string
		wantAbsent []string
		wantSent   []request
	}{
		{
			name:       "edit on an empty list opens no form",
			rows:       []proto.Message{newLiveWorkflow("w1", "api")},
			press:      []string{"e"},
			wantDrawn:  []string{"no schedules yet"},
			wantAbsent: []string{scheduleForm},
		},
		{
			name:       "archive on an empty list opens no prompt",
			rows:       []proto.Message{newLiveWorkflow("w1", "api")},
			press:      []string{"z"},
			wantAbsent: []string{"Archive schedule"},
		},
		{
			name:  "toggle on an empty list asks for no write",
			rows:  []proto.Message{newLiveWorkflow("w1", "api")},
			press: []string{"space"},
		},
		{
			name:       "new with no workflows says why rather than opening the form",
			press:      append(widenListPresses, "c"),
			wantDrawn:  []string{"Could not create a schedule"},
			wantAbsent: []string{scheduleForm},
		},
		{
			name:       "back returns to the agents tree",
			rows:       buildSchedules(),
			press:      []string{"esc"},
			wantScreen: agentsScreen,
		},
		{
			name:      "new opens the form on the first workflow",
			rows:      buildSchedules(),
			press:     []string{"c"},
			wantDrawn: []string{scheduleForm, "› api"},
		},
		{
			name:      "edit opens the form over the hovered schedule",
			rows:      buildSchedules(),
			press:     []string{"e"},
			wantDrawn: []string{"> morning-ci", "> 0 9 * * 1-5", "check CI"},
		},
		{
			name:      "archive asks first",
			rows:      buildSchedules(),
			press:     []string{"z"},
			wantDrawn: []string{archiveAsk},
		},
		{
			name:       "archive once confirmed archives the hovered schedule and lands back on the schedules",
			rows:       buildSchedules(),
			press:      []string{"z", "y"},
			wantScreen: schedulesScreen,
			wantAbsent: []string{archiveAsk},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceArchiveScheduleProcedure,
				msg:       &schedulev1.ArchiveScheduleRequest{Id: "s1"},
			}},
		},
		{
			name:  "toggle pauses an armed schedule with no form in between",
			rows:  buildSchedules(),
			press: []string{"space"},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure,
				msg:       &schedulev1.SetScheduleEnabledRequest{Id: "s1"},
			}},
		},
		{
			name:  "toggle resumes a paused schedule",
			rows:  []proto.Message{newLiveWorkflow("w1", "api"), newPausedSchedule("s1", "nightly")},
			press: []string{"space"},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure,
				msg: &schedulev1.SetScheduleEnabledRequest{
					Id:      "s1",
					Enabled: true,
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openSchedules(t, tt.rows...)

			harness.press(tt.press...)

			if tt.wantScreen != "" {
				assert.Equal(t, tt.wantScreen, harness.readScreen())
			}
			frame := harness.readFrame()
			for _, drawn := range tt.wantDrawn {
				assert.Contains(t, frame, drawn)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.assertSent(tt.wantSent...)
		})
	}
}

// TestSchedulesList covers the rows the list draws and the cursor keeping its schedule.
func TestSchedulesList(t *testing.T) {
	t.Run("each schedule shows whether it is armed, its name and its expression", func(t *testing.T) {
		harness := openSchedules(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"), newPausedSchedule("s2", "nightly"))

		frame := harness.readFrame()
		assert.Equal(t, schedulesScreen, harness.readScreen())
		assert.Equal(t, []string{"◇ nightly  0 1 * * *", "◆ morning-ci  0 9 * * 1-5"}, readScheduleRows(frame))
	})

	t.Run("an archived schedule is left to the archive", func(t *testing.T) {
		harness := openSchedules(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"), newArchivedSchedule("s2", "weekly-gone"))

		assert.Equal(t, []string{"◆ morning-ci  0 9 * * 1-5"}, readScheduleRows(harness.readFrame()))
	})

	t.Run("the cursor keeps its schedule as another lands above it", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"))
		harness.press("j")
		require.Equal(t, "alpha", readHoveredSchedule(harness.readFrame()))

		harness.watch(buildPuts(newSchedule("c", "charlie")))

		assert.Equal(t, "alpha", readHoveredSchedule(harness.readFrame()))
	})

	t.Run("a schedule gone from under the cursor leaves it on the last row", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"), newSchedule("c", "charlie"))
		harness.press("j", "j")
		require.Equal(t, "alpha", readHoveredSchedule(harness.readFrame()))

		harness.watch(buildSnapshot(newSchedule("b", "bravo"), newSchedule("c", "charlie")))

		assert.Equal(t, "bravo", readHoveredSchedule(harness.readFrame()))
	})

	t.Run("the document beside the list follows the cursor", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"))
		harness.press("j")

		assert.Contains(t, readStageText(harness.readFrame()), "alpha")
	})
}

// TestSchedulesDocument covers the document drawn beside the list for the hovered schedule.
func TestSchedulesDocument(t *testing.T) {
	next := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.Local)

	hidden := newSchedule("s1", "quiet")
	hidden.HideAgents = true

	armed := newSchedule("s1", "morning-ci")
	armed.NextRunAt = timestamppb.New(next)

	bare := newSchedule("s1", "bare")
	bare.Prompt = ""

	flaky := newSchedule("s1", "flaky")
	flaky.RecentRuns = []*schedulev1.ScheduleRun{
		{
			ScheduledFor: timestamppb.New(next),
			State:        schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_DISCARDED,
			Attempt:      5,
			MaxAttempts:  5,
			Message:      "bad directory",
		},
		{
			ScheduledFor: timestamppb.New(next.Add(-time.Hour)),
			State:        schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED,
			Attempt:      1,
			MaxAttempts:  5,
			Message:      "schedule is paused",
		},
		{
			ScheduledFor: timestamppb.New(next.Add(-2 * time.Hour)),
			State:        schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED,
			Attempt:      2,
			MaxAttempts:  5,
			Message:      "an earlier attempt's error",
		},
	}

	tests := []struct {
		name       string
		schedule   *schedulev1.Schedule
		wantDrawn  []string
		wantAbsent []string
	}{
		{
			name:      "an empty list says how to write one",
			wantDrawn: []string{"A schedule launches an agent, or mails an orchestrator, on a cron expression."},
		},
		{
			name:       "a paused schedule says it will not run",
			schedule:   newPausedSchedule("s1", "nightly"),
			wantDrawn:  []string{"nightly", "paused — will not run"},
			wantAbsent: []string{"next run"},
		},
		{
			name:      "a schedule with no prompt says so rather than paging a blank",
			schedule:  bare,
			wantDrawn: []string{"bare", "no prompt set"},
		},
		{
			name:      "one whose runs start hidden says so",
			schedule:  hidden,
			wantDrawn: []string{"quiet", "Its runs start hidden."},
		},
		{
			name:      "a schedule that has never fired says so in one line",
			schedule:  newSchedule("s1", "fresh"),
			wantDrawn: []string{"never fired", "check CI"},
		},
		{
			name:     "each run reads as its outcome, a skip giving its reason and a failure its error",
			schedule: flaky,
			wantDrawn: []string{
				"07-28 09:00  failed     5/5 attempts",
				"bad directory",
				"07-28 08:00  skipped    schedule is paused",
				"07-28 07:00  launched   2 attempts",
			},
			wantAbsent: []string{"an earlier attempt's error"},
		},
		{
			name:       "an armed schedule leads with when it next fires",
			schedule:   armed,
			wantDrawn:  []string{"morning-ci", "next run", "check CI"},
			wantAbsent: []string{"paused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []proto.Message{newLiveWorkflow("w1", "api")}
			if tt.schedule != nil {
				rows = append(rows, tt.schedule)
			}
			harness := openSchedules(t, rows...)

			document := readStageText(harness.readFrame())
			for _, drawn := range tt.wantDrawn {
				assert.Contains(t, document, drawn)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, document, absent)
			}
		})
	}
}

// TestSchedulesRecord covers the fields the record under the list draws for a schedule.
func TestSchedulesRecord(t *testing.T) {
	configured := &workflowv1.Workflow{
		Id:      "w1",
		Name:    "api",
		Harness: "codex",
		Cwd:     "/work/api",
		Flags:   "--model opus",
	}

	zoned := newSchedule("s1", "morning-ci")
	zoned.Timezone = "UTC"

	mailing := newSchedule("s1", "morning-ci")
	mailing.Timezone = "UTC"
	mailing.Kind = schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR
	mailing.OrchestratorAgentId = proto.String("o1")

	tests := []struct {
		name     string
		schedule *schedulev1.Schedule
		workflow *workflowv1.Workflow
		agent    *agentv1.Agent
		want     []string
	}{
		{
			name:     "an orchestrator schedule names the orchestrator it mails in place of the workflow",
			schedule: mailing,
			workflow: configured,
			agent:    newOrchestrator(),
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind orchestrator",
				"mails orchestrator",
			},
		},
		{
			name:     "an orchestrator schedule whose orchestrator is gone says so",
			schedule: mailing,
			workflow: configured,
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind orchestrator",
				"mails — (gone)",
			},
		},
		{
			name: "a schedule whose workflow was archived says so rather than showing a blank",
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind implementer",
				"workflow — (archived)",
			},
		},
		{
			name:     "a workflow with no defaults falls back to dashes",
			workflow: newLiveWorkflow("w1", "api"),
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind implementer",
				"workflow api",
				"default agent —",
				"directory —",
				"flags —",
				"worktree off",
			},
		},
		{
			name:     "its own settings come first, then what the workflow hands each run",
			workflow: configured,
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind implementer",
				"workflow api",
				"default agent codex",
				"directory /work/api",
				"flags --model opus",
				"worktree off",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule := zoned
			if tt.schedule != nil {
				schedule = tt.schedule
			}
			rows := []proto.Message{schedule}
			if tt.workflow != nil {
				rows = append(rows, tt.workflow)
			}
			if tt.agent != nil {
				rows = append(rows, tt.agent)
			}
			harness := openSchedules(t, rows...)

			assert.Equal(t, tt.want, trimRecord(readRecord(harness.readFrame())))
		})
	}
}

// TestSchedulesLaunchedAgent covers an agent's record naming the schedule that launched it.
func TestSchedulesLaunchedAgent(t *testing.T) {
	tests := []struct {
		name       string
		scheduleID string
		want       string
	}{
		{
			name:       "an agent whose schedule has since gone names nothing",
			scheduleID: "gone",
		},
		{
			name:       "an agent a schedule launched names it in its own record",
			scheduleID: "s1",
			want:       "morning-ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launched := newAgent("a1", "morning-ci-1", "w1")
			launched.ScheduleId = proto.String(tt.scheduleID)
			harness := openAgents(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"), launched)
			harness.press("j")

			assert.Equal(t, tt.want, readRecordField(harness.readFrame(), "launched by"))
		})
	}
}

// TestSchedulesForm covers writing a schedule through its form.
func TestSchedulesForm(t *testing.T) {
	t.Run("a refused create says why and keeps what was typed", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.refuse(schedulev1connect.ScheduleServiceCreateScheduleProcedure, errors.New("cron must be five fields"))

		harness.press(widenListPresses...)
		harness.press("c", "tab")
		typeText(harness, "x")
		harness.press("enter")
		typeText(harness, "0 9 * * *")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "x",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
		frame := harness.readFrame()
		assert.Contains(t, frame, "Could not create the schedule")
		assert.Contains(t, readStageText(frame), "> x")
		assert.Contains(t, readStageText(frame), "> 0 9 * * *")
	})

	t.Run("an orchestrator schedule mails the orchestrator picked in place of a workflow", func(t *testing.T) {
		harness := openSchedules(t, append(buildSchedules(), newOrchestrator())...)

		harness.press(widenListPresses...)
		harness.press("c", "down")
		form := readStageText(harness.readFrame())
		assert.Contains(t, form, "› orchestrator  api")
		assert.NotContains(t, form, "┃ Workflow")
		assert.NotContains(t, form, "Hide its runs?")

		harness.press("tab")
		typeText(harness, "nightly")
		harness.press("enter")
		typeText(harness, "0 9 * * *")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "nightly",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: "o1"},
		})
	})

	t.Run("with no live orchestrator only an implementer schedule is offered", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)

		harness.press(widenListPresses...)
		harness.press("c", "down")
		form := readStageText(harness.readFrame())
		assert.Contains(t, form, "start an orchestrator to mail one instead")
		assert.NotContains(t, form, "mail one already running")

		harness.press("tab")
		typeText(harness, "nightly")
		harness.press("enter")
		typeText(harness, "0 9 * * *")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "nightly",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
	})

	t.Run("flipping the kind keeps what was typed and the cursor on the kind", func(t *testing.T) {
		harness := openSchedules(t, append(buildSchedules(), newOrchestrator())...)

		harness.press(widenListPresses...)
		harness.press("c", "tab")
		typeText(harness, "nightly")
		harness.press("enter")
		typeText(harness, "0 9 * * *")
		harness.press("enter", "enter")
		typeText(harness, "check CI")
		harness.press("shift+tab", "shift+tab", "shift+tab", "shift+tab", "down")
		require.Contains(t, readStageText(harness.readFrame()), "› orchestrator  api")
		harness.press("up")
		require.Contains(t, readStageText(harness.readFrame()), "› api")
		harness.press("down")

		form := readStageText(harness.readFrame())
		assert.Contains(t, form, "> nightly")
		assert.Contains(t, form, "check CI")
		harness.press("tab")
		typeText(harness, "-brief")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "nightly-brief",
			Cron:    "0 9 * * *",
			Prompt:  "check CI",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: "o1"},
		})
	})

	t.Run("an edit of an orchestrator schedule keeps the orchestrator it mails", func(t *testing.T) {
		mailing := newSchedule("s1", "morning-ci")
		mailing.Kind = schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR
		mailing.OrchestratorAgentId = proto.String("o1")
		harness := openSchedules(t, newLiveWorkflow("w1", "api"), mailing, newOrchestrator())

		harness.press("e")
		require.Contains(t, readStageText(harness.readFrame()), "› orchestrator  api")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
			Id:      "s1",
			Name:    "morning-ci",
			Cron:    "0 9 * * 1-5",
			Enabled: true,
			Prompt:  "check CI",
			Target:  &schedulev1.UpdateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: "o1"},
		})
	})

	t.Run("a refused pause says why", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.refuse(schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure, errors.New("cron must be five fields"))

		harness.press(widenListPresses...)
		harness.press("space")

		assert.Contains(t, harness.readFrame(), `Could not pause schedule "morning-ci"`)
	})

	t.Run("an edit sends the field it changed and the flags it was opened with", func(t *testing.T) {
		hidden := newSchedule("s1", "morning-ci")
		hidden.HideAgents = true
		harness := openSchedules(t, newLiveWorkflow("w1", "api"), hidden)

		harness.press("e", "tab", "tab", "tab", "tab")
		typeText(harness, " harder")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
			Id:         "s1",
			Name:       "morning-ci",
			Cron:       "0 9 * * 1-5",
			Enabled:    true,
			HideAgents: true,
			Prompt:     "check CI harder",
			Target:     &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
	})

	t.Run("every confirm draws its buttons in the same column", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.press("e")

		var indents []int
		for _, line := range readStage(harness.readFrame()) {
			buttons := strings.TrimLeft(strings.TrimLeft(line, "┃"), " ")
			if strings.HasPrefix(buttons, "Armed ") || strings.HasPrefix(buttons, "Hidden ") {
				indents = append(indents, len(line)-len(buttons))
			}
		}

		require.Len(t, indents, 2, "one row of buttons per confirm")
		assert.Equal(t, indents[0], indents[1])
	})

	t.Run("a workflow made while the form is up is offered and the cursor stays put", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.press("e", "tab", "tab")

		harness.watch(buildPuts(newLiveWorkflow("w2", "brandnew")))

		form := readStageText(harness.readFrame())
		assert.Contains(t, form, "brandnew  —")
		assert.Contains(t, form, "┃ Cron")
	})

	t.Run("a schedule keeps the workflow it names when that one is archived under the form", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.press("e")

		harness.watch(buildSnapshot(newLiveWorkflow("w2", "other"), newSchedule("s1", "morning-ci")))
		require.Contains(t, readStageText(harness.readFrame()), "w1  gone")
		harness.press("ctrl+s")

		harness.assertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
			Id:      "s1",
			Name:    "morning-ci",
			Cron:    "0 9 * * 1-5",
			Enabled: true,
			Prompt:  "check CI",
			Target:  &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
	})

	t.Run("a schedule on any workflow but the first is offered every one and stays on its own", func(t *testing.T) {
		second := newSchedule("s1", "bughunt-fixer-dispatcher")
		second.WorkflowId = "w2"
		harness := openSchedules(t,
			&workflowv1.Workflow{
				Id:   "w1",
				Name: "bughunt",
				Cwd:  "/repos/komrad",
			},
			&workflowv1.Workflow{
				Id:   "w2",
				Name: "komrad",
				Cwd:  "/repos/komrad",
			},
			second,
		)
		harness.press("e")

		form := readStageText(harness.readFrame())
		assert.Contains(t, form, "bughunt  /repos/komrad")
		assert.Contains(t, form, "› komrad  /repos/komrad")
	})
}

func openSchedules(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openAgents(t, rows...)
	harness.press("o", "s")

	return harness
}

func buildSchedules() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "api"),
		newSchedule("s1", "morning-ci"),
	}
}

func newSchedule(id, name string) *schedulev1.Schedule {
	return &schedulev1.Schedule{
		Id:         id,
		Name:       name,
		Cron:       "0 9 * * 1-5",
		Enabled:    true,
		Prompt:     "check CI",
		WorkflowId: "w1",
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
	}
}

func newPausedSchedule(id, name string) *schedulev1.Schedule {
	schedule := newSchedule(id, name)
	schedule.Cron = "0 1 * * *"
	schedule.Enabled = false

	return schedule
}

func readScheduleRows(frame string) []string {
	var rows []string
	for _, line := range readSidebar(frame) {
		if row := strings.TrimSpace(strings.TrimPrefix(line, "›")); strings.HasPrefix(row, "◆") || strings.HasPrefix(row, "◇") {
			rows = append(rows, row)
		}
	}

	return rows
}

func readHoveredSchedule(frame string) string {
	for _, line := range readSidebar(frame) {
		if row, isHovered := strings.CutPrefix(line, "›"); isHovered {
			return strings.Fields(row)[1]
		}
	}

	return ""
}

func trimRecord(record []string) []string {
	var fields []string
	for _, line := range record {
		if field := strings.Fields(strings.TrimPrefix(line, "›")); len(field) > 0 {
			fields = append(fields, strings.Join(field, " "))
		}
	}

	return fields
}
