package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestSchedulesKeys covers what each key does on the schedule under the cursor.
func TestSchedulesKeys(t *testing.T) {
	tests := []struct {
		name         string
		rows         []proto.Message
		press        []string
		wantScreen   *screen.ID
		wantRendered []string
		wantAbsent   []string
		gone         *watchv1.WatchEvent
		wantSent     []paneltest.Request
	}{
		{
			name:         "edit on an empty list opens no form",
			rows:         []proto.Message{paneltest.Workflow("w1", "api")},
			press:        []string{"space", "e"},
			wantRendered: []string{"no schedules yet"},
			wantAbsent:   []string{scheduleForm},
		},
		{
			name:       "archive on an empty list opens no prompt",
			rows:       []proto.Message{paneltest.Workflow("w1", "api")},
			press:      []string{"space", "a"},
			wantAbsent: []string{"Archive schedule"},
		},
		{
			name:  "pause on an empty list asks for no write",
			rows:  []proto.Message{paneltest.Workflow("w1", "api")},
			press: []string{"space", "p"},
		},
		{
			name:         "new with no workflows says why rather than opening the form",
			press:        append(paneltest.WidenListPresses, "space", "c"),
			wantRendered: []string{"Could not create a schedule"},
			wantAbsent:   []string{scheduleForm},
		},
		{
			name:       "back opens the view switcher",
			rows:       buildSchedules(),
			press:      []string{"esc", "shift+tab"},
			wantScreen: xptr.Ptr(screen.Agents),
		},
		{
			name:         "new opens the form on the first workflow",
			rows:         buildSchedules(),
			press:        []string{"space", "c"},
			wantRendered: []string{scheduleForm, "› api"},
		},
		{
			name:         "edit opens the form over the hovered schedule",
			rows:         buildSchedules(),
			press:        []string{"space", "e"},
			wantRendered: []string{"> morning-ci", "> 0 9 * * 1-5", "check CI"},
		},
		{
			name:         "edit on a kind a newer server added says why rather than opening the form",
			rows:         []proto.Message{paneltest.Workflow("w1", "api"), newUnknownKindSchedule()},
			press:        []string{"space", "e"},
			wantRendered: []string{"Could not edit schedule"},
			wantAbsent:   []string{scheduleForm},
		},
		{
			name:         "rename on a kind a newer server added sends nothing",
			rows:         []proto.Message{paneltest.Workflow("w1", "api"), newUnknownKindSchedule()},
			press:        slices.Concat([]string{"r"}, spellKeys("-2"), []string{"enter"}),
			wantRendered: []string{"Could not rename schedule"},
		},
		{
			name:         "archive opens its menu",
			rows:         buildSchedules(),
			press:        []string{"space", "a"},
			wantRendered: []string{archiveMenu},
		},
		{
			name:       "archive from its menu archives the hovered schedule and lands back on the schedules",
			rows:       buildSchedules(),
			press:      []string{"space", "a", "a"},
			gone:       paneltest.Deleted(watchv1.Resource_RESOURCE_SCHEDULE, "s1"),
			wantScreen: xptr.Ptr(screen.Schedules),
			wantAbsent: []string{archiveMenu},
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceArchiveScheduleProcedure,
				Msg:       &schedulev1.ArchiveScheduleRequest{Id: "s1"},
			}},
		},
		{
			name:         "rename types over the hovered schedule's name",
			rows:         buildSchedules(),
			press:        slices.Concat([]string{"r"}, spellKeys("-2")),
			wantRendered: []string{"morning-ci-2▏", "enter save"},
		},
		{
			name:         "rename on a long name keeps the cursor in view near its start",
			rows:         []proto.Message{paneltest.Workflow("w1", "api"), newSchedule("s1", "abcdefghijklmnopqrstuvwxyz0123456789")},
			press:        []string{"r", "home", "right"},
			wantRendered: []string{"a[b]cdefghijklmnopqrstuvwxyz…"},
		},
		{
			name:  "rename sends the typed name with the rest of the schedule as it was",
			rows:  buildSchedules(),
			press: slices.Concat([]string{"r"}, spellKeys("-2"), []string{"enter"}),
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceUpdateScheduleProcedure,
				Msg: &schedulev1.UpdateScheduleRequest{
					Id:      "s1",
					Name:    "morning-ci-2",
					Cron:    "0 9 * * 1-5",
					Enabled: true,
					Prompt:  "check CI",
					Target:  &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: "w1"},
				},
			}},
		},
		{
			name:  "pause from the menu pauses an armed schedule",
			rows:  buildSchedules(),
			press: []string{"space", "p"},
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure,
				Msg:       &schedulev1.SetScheduleEnabledRequest{Id: "s1"},
			}},
		},
		{
			name:         "the menu offers resume on a paused schedule",
			rows:         []proto.Message{paneltest.Workflow("w1", "api"), newPausedSchedule("s1", "weekly-report")},
			press:        []string{"space"},
			wantRendered: []string{"resume"},
		},
		{
			name:  "resume from the menu resumes a paused schedule",
			rows:  []proto.Message{paneltest.Workflow("w1", "api"), newPausedSchedule("s1", "nightly")},
			press: []string{"space", "p"},
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure,
				Msg: &schedulev1.SetScheduleEnabledRequest{
					Id:      "s1",
					Enabled: true,
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openSchedules(t, tt.rows...)

			harness.Press(tt.press...)
			if tt.gone != nil {
				require.Contains(t, harness.TypedFrame(), archiveMenu, "the menu waits for the schedule to leave")
				harness.Watch(tt.gone)
			}

			if tt.wantScreen != nil {
				assert.Equal(t, *tt.wantScreen, paneltest.Screen(harness.Frame()))
			}
			frame := harness.TypedFrame()
			for _, rendered := range tt.wantRendered {
				assert.Contains(t, frame, rendered)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.AssertSent(tt.wantSent...)
		})
	}
}

// TestSchedulesList covers the rows the list renders and the cursor keeping its schedule.
func TestSchedulesList(t *testing.T) {
	t.Run("each schedule shows whether it is armed, its name and its expression", func(t *testing.T) {
		harness := openSchedules(t, paneltest.Workflow("w1", "api"), newSchedule("s1", "morning-ci"), newPausedSchedule("s2", "nightly"))

		frame := harness.Frame()
		assert.Equal(t, screen.Schedules, paneltest.Screen(harness.Frame()))
		assert.Equal(t, []string{"◇ nightly 0 1 * * *", "◆ morning-ci 0 9 * * 1-5"}, readScheduleRows(frame))
	})

	t.Run("an archived schedule is left to the archive", func(t *testing.T) {
		harness := openSchedules(t, paneltest.Workflow("w1", "api"), newSchedule("s1", "morning-ci"), newArchivedSchedule("weekly-gone"))

		assert.Equal(t, []string{"◆ morning-ci 0 9 * * 1-5"}, readScheduleRows(harness.Frame()))
	})

	t.Run("the cursor keeps its schedule as another lands above it", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"))
		harness.Press("j")
		require.Equal(t, "alpha", readHoveredSchedule(harness.Frame()))

		harness.Watch(paneltest.Puts(newSchedule("c", "charlie")))

		assert.Equal(t, "alpha", readHoveredSchedule(harness.Frame()))
	})

	t.Run("a schedule gone from under the cursor leaves it on the last row", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"), newSchedule("c", "charlie"))
		harness.Press("j", "j")
		require.Equal(t, "alpha", readHoveredSchedule(harness.Frame()))

		harness.Watch(paneltest.Snapshot(newSchedule("b", "bravo"), newSchedule("c", "charlie")))

		assert.Equal(t, "bravo", readHoveredSchedule(harness.Frame()))
	})

	t.Run("the document beside the list follows the cursor", func(t *testing.T) {
		harness := openSchedules(t, newSchedule("a", "alpha"), newSchedule("b", "bravo"))
		harness.Press("j")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "alpha")
	})
}

// TestSchedulesFilter covers the qualifiers the schedules filter reads.
func TestSchedulesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantRows   []string
		wantSearch string
	}{
		{
			name:       "name: keeps the schedules whose name holds it",
			query:      "name:CI",
			wantRows:   []string{"◆ morning-ci 0 9 * * 1-5"},
			wantSearch: "▽ name:CI",
		},
		{
			name:       "workflow: keeps the schedules launching into it",
			query:      "workflow:web",
			wantRows:   []string{"◇ nightly 0 1 * * *"},
			wantSearch: "▽ workflow:web",
		},
		{
			name:       "workflow: naming none keeps nothing",
			query:      "workflow:elsewhere",
			wantSearch: "▽ workflow:elsewhere",
		},
		{
			name:       "is:paused keeps the paused schedules",
			query:      "is:paused",
			wantRows:   []string{"◇ nightly 0 1 * * *"},
			wantSearch: "▽ is:paused",
		},
		{
			name:       "-is:paused keeps the armed schedules",
			query:      "-is:paused",
			wantRows:   []string{"◆ morning-ci 0 9 * * 1-5"},
			wantSearch: "▽ -is:paused",
		},
		{
			name:       "an is: other than paused is a problem on the line and takes nothing off",
			query:      "is:armed",
			wantRows:   []string{"◇ nightly 0 1 * * *", "◆ morning-ci 0 9 * * 1-5"},
			wantSearch: "▽ is:armed  is:",
		},
		{
			name:       "an unknown qualifier is a problem on the line and takes nothing off",
			query:      "agent:quiet",
			wantRows:   []string{"◇ nightly 0 1 * * *", "◆ morning-ci 0 9 * * 1-5"},
			wantSearch: "▽ agent:quiet  agent: is not a qualifier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nightly := newPausedSchedule("s2", "nightly")
			nightly.WorkflowId = "w2"
			harness := openSchedules(t, paneltest.Workflow("w1", "api"), paneltest.Workflow("w2", "web"), newSchedule("s1", "morning-ci"), nightly)

			harness.Press(slices.Concat([]string{"/", "tab"}, spellKeys(tt.query), []string{"enter"})...)

			frame := harness.Frame()
			assert.Equal(t, tt.wantRows, readScheduleRows(frame))
			assert.True(t, strings.HasPrefix(paneltest.Search(frame), tt.wantSearch), paneltest.Search(frame))
		})
	}
}

// TestSchedulesDocument covers the document rendered beside the list for the hovered schedule.
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
		name         string
		schedule     *schedulev1.Schedule
		wantRendered []string
		wantAbsent   []string
	}{
		{
			name:         "an empty list says how to write one",
			wantRendered: []string{"A schedule launches an agent, or mails an orchestrator, on a cron expression."},
		},
		{
			name:         "a paused schedule says it will not run",
			schedule:     newPausedSchedule("s1", "nightly"),
			wantRendered: []string{"nightly", "paused — will not run"},
			wantAbsent:   []string{"next run"},
		},
		{
			name:         "a schedule with no prompt says so rather than paging a blank",
			schedule:     bare,
			wantRendered: []string{"bare", "no prompt set"},
		},
		{
			name:         "one whose runs start hidden says so",
			schedule:     hidden,
			wantRendered: []string{"quiet", "Its runs start hidden."},
		},
		{
			name:         "a schedule that has never fired says so in one line",
			schedule:     newSchedule("s1", "fresh"),
			wantRendered: []string{"never fired", "check CI"},
		},
		{
			name:     "each run reads as its outcome, a skip giving its reason and a failure its error",
			schedule: flaky,
			wantRendered: []string{
				"07-28 09:00  failed     5/5 attempts",
				"bad directory",
				"07-28 08:00  skipped    schedule is paused",
				"07-28 07:00  launched   2 attempts",
			},
			wantAbsent: []string{"an earlier attempt's error"},
		},
		{
			name:         "an armed schedule leads with when it next fires",
			schedule:     armed,
			wantRendered: []string{"morning-ci", "next run", "check CI"},
			wantAbsent:   []string{"paused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []proto.Message{paneltest.Workflow("w1", "api")}
			if tt.schedule != nil {
				rows = append(rows, tt.schedule)
			}
			harness := openSchedules(t, rows...)

			document := paneltest.StageText(harness.Frame())
			for _, rendered := range tt.wantRendered {
				assert.Contains(t, document, rendered)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, document, absent)
			}
		})
	}
}

// TestSchedulesRecord covers the fields the record under the list renders for a schedule.
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
	mailing.OrchestratorAgentId = xptr.Ptr("o1")

	unknown := newUnknownKindSchedule()
	unknown.Timezone = "UTC"

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
			agent:    paneltest.Orchestrator(),
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
			name:     "a schedule of a kind a newer server added says its kind is unknown",
			schedule: unknown,
			workflow: configured,
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind unknown",
			},
		},
		{
			name: "a schedule whose workflow was archived says so rather than showing a blank",
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind task",
				"workflow — (archived)",
			},
		},
		{
			name:     "a workflow with no defaults falls back to dashes",
			workflow: paneltest.Workflow("w1", "api"),
			want: []string{
				"name morning-ci",
				"cron 0 9 * * 1-5",
				"timezone UTC",
				"kind task",
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
				"kind task",
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

			assert.Equal(t, tt.want, trimRecord(paneltest.Record(harness.Frame())))
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
			launched := paneltest.Agent("a1", "morning-ci-1", "w1")
			launched.ScheduleId = xptr.Ptr(tt.scheduleID)
			harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), newSchedule("s1", "morning-ci"), launched)
			harness.Press("j")

			assert.Equal(t, tt.want, paneltest.RecordField(harness.Frame(), "launched by"))
		})
	}
}

// TestSchedulesForm covers writing a schedule through its form.
func TestSchedulesForm(t *testing.T) {
	t.Run("a refused create says why and keeps what was typed", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.Refuse(schedulev1connect.ScheduleServiceCreateScheduleProcedure, errors.New("cron must be five fields"))

		harness.Press(paneltest.WidenListPresses...)
		harness.Press("space", "c", "tab")
		harness.Type("x")
		harness.Press("tab")
		harness.Type("0 9 * * *")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "x",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
		frame := harness.Frame()
		assert.Contains(t, frame, "Could not create the schedule")
		assert.Contains(t, paneltest.StageText(frame), "> x")
		assert.Contains(t, paneltest.StageText(frame), "> 0 9 * * *")
	})

	t.Run("an orchestrator schedule mails the orchestrator picked in place of a workflow", func(t *testing.T) {
		harness := openSchedules(t, append(buildSchedules(), paneltest.Orchestrator())...)

		harness.Press(paneltest.WidenListPresses...)
		harness.Press("space", "c", "down")
		form := paneltest.StageText(harness.Frame())
		assert.Contains(t, form, "› orchestrator  api")
		assert.NotContains(t, form, "  Workflow")
		assert.NotContains(t, form, "Hide its tasks?")

		harness.Press("tab")
		harness.Type("nightly")
		harness.Press("tab")
		harness.Type("0 9 * * *")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "nightly",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: "o1"},
		})
	})

	t.Run("with no live orchestrator only a task schedule is offered", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)

		harness.Press(paneltest.WidenListPresses...)
		harness.Press("space", "c", "down")
		form := paneltest.StageText(harness.Frame())
		assert.Contains(t, form, "start an orchestrator to mail one instead")
		assert.NotContains(t, form, "mail one already running")

		harness.Press("tab")
		harness.Type("nightly")
		harness.Press("tab")
		harness.Type("0 9 * * *")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
			Name:    "nightly",
			Cron:    "0 9 * * *",
			Enabled: true,
			Target:  &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
	})

	t.Run("flipping the kind keeps what was typed and the cursor on the kind", func(t *testing.T) {
		harness := openSchedules(t, append(buildSchedules(), paneltest.Orchestrator())...)

		harness.Press(paneltest.WidenListPresses...)
		harness.Press("space", "c", "tab")
		harness.Type("nightly")
		harness.Press("tab")
		harness.Type("0 9 * * *")
		harness.Press("tab", "tab")
		harness.Type("check CI")
		harness.Press("shift+tab", "shift+tab", "shift+tab", "shift+tab", "down")
		require.Contains(t, paneltest.StageText(harness.Frame()), "› orchestrator  api")
		harness.Press("up")
		require.Contains(t, paneltest.StageText(harness.Frame()), "› api")
		harness.Press("down")

		form := paneltest.StageText(harness.Frame())
		assert.Contains(t, form, "> nightly")
		assert.Contains(t, form, "check CI")
		harness.Press("tab")
		harness.Type("-brief")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceCreateScheduleProcedure, &schedulev1.CreateScheduleRequest{
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
		mailing.OrchestratorAgentId = xptr.Ptr("o1")
		harness := openSchedules(t, paneltest.Workflow("w1", "api"), mailing, paneltest.Orchestrator())

		harness.Press("space", "e")
		require.Contains(t, paneltest.StageText(harness.Frame()), "› orchestrator  api")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
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
		harness.Refuse(schedulev1connect.ScheduleServiceSetScheduleEnabledProcedure, errors.New("cron must be five fields"))

		harness.Press(paneltest.WidenListPresses...)
		harness.Press("space", "p")

		assert.Contains(t, harness.Frame(), `Could not pause schedule "morning-ci"`)
	})

	t.Run("an edit sends the field it changed and the flags it was opened with", func(t *testing.T) {
		hidden := newSchedule("s1", "morning-ci")
		hidden.HideAgents = true
		harness := openSchedules(t, paneltest.Workflow("w1", "api"), hidden)

		harness.Press("space", "e", "tab", "tab", "tab", "tab")
		harness.Type(" harder")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
			Id:         "s1",
			Name:       "morning-ci",
			Cron:       "0 9 * * 1-5",
			Enabled:    true,
			HideAgents: true,
			Prompt:     "check CI harder",
			Target:     &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: "w1"},
		})
	})

	t.Run("every confirm renders its buttons in the same column", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.Press("space", "e")

		var indents []int
		for _, line := range paneltest.Stage(harness.Frame()) {
			buttons := strings.TrimLeft(line, " ")
			if strings.HasPrefix(buttons, "Armed ") || strings.HasPrefix(buttons, "Hidden ") {
				indents = append(indents, len(line)-len(buttons))
			}
		}

		require.Len(t, indents, 2, "one row of buttons per confirm")
		assert.Equal(t, indents[0], indents[1])
	})

	t.Run("a workflow made while the form is up is offered and the cursor stays put", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.Press("space", "e", "tab", "tab")

		harness.Watch(paneltest.Puts(paneltest.Workflow("w2", "brandnew")))

		harness.Type("x")

		form := paneltest.StageText(harness.Frame())
		assert.Contains(t, form, "brandnew  —")
		assert.Contains(t, form, "> 0 9 * * 1-5x")
	})

	t.Run("a schedule keeps the workflow it names when that one is archived under the form", func(t *testing.T) {
		harness := openSchedules(t, buildSchedules()...)
		harness.Press("space", "e")

		harness.Watch(paneltest.Snapshot(paneltest.Workflow("w2", "other"), newSchedule("s1", "morning-ci")))
		require.Contains(t, paneltest.StageText(harness.Frame()), "w1  gone")
		harness.Press("enter")

		harness.AssertSentTo(schedulev1connect.ScheduleServiceUpdateScheduleProcedure, &schedulev1.UpdateScheduleRequest{
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
		harness.Press("space", "e")

		form := paneltest.StageText(harness.Frame())
		assert.Contains(t, form, "bughunt  /repos/komrad")
		assert.Contains(t, form, "› komrad  /repos/komrad")
	})
}

const (
	scheduleForm = "minute hour day month weekday"
	archiveMenu  = "morning-ci › archive"
)

func openSchedules(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, rows...)
	harness.Press("tab")

	return harness
}

func buildSchedules() []proto.Message {
	return []proto.Message{
		paneltest.Workflow("w1", "api"),
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
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
	}
}

func newUnknownKindSchedule() *schedulev1.Schedule {
	schedule := newSchedule("s1", "morning-ci")
	schedule.Kind = schedulev1.ScheduleKind(99)

	return schedule
}

func newPausedSchedule(id, name string) *schedulev1.Schedule {
	schedule := newSchedule(id, name)
	schedule.Cron = "0 1 * * *"
	schedule.Enabled = false

	return schedule
}

func readScheduleRows(frame string) []string {
	var rows []string
	for _, line := range paneltest.Sidebar(frame) {
		if row := strings.TrimSpace(strings.TrimPrefix(line, "›")); strings.HasPrefix(row, "◆") || strings.HasPrefix(row, "◇") {
			rows = append(rows, row)
		}
	}

	return rows
}

func readHoveredSchedule(frame string) string {
	for _, line := range paneltest.Sidebar(frame) {
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
