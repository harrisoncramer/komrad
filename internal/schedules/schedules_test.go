package schedules

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseCron covers what the scheduler accepts.
func TestParseCron(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		timezone   string
		wantErr    bool
	}{
		{
			name:       "a weekday morning",
			expression: "0 9 * * 1-5",
		},
		{
			name:       "a step",
			expression: "*/15 * * * *",
		},
		{
			name:       "a named zone is applied",
			expression: "0 9 * * *",
			timezone:   "America/New_York",
		},
		{
			name:       "an hour that does not exist",
			expression: "0 99 * * *",
			wantErr:    true,
		},
		{
			name:       "a field that is not a number",
			expression: "0 nine * * *",
			wantErr:    true,
		},
		{
			name:       "a zone nobody has heard of",
			expression: "0 9 * * *",
			timezone:   "Mars/Olympus",
			wantErr:    true,
		},
		{
			name:       "February the thirtieth",
			expression: "0 0 30 2 *",
			wantErr:    true,
		},
		{
			name:       "February the thirty-first",
			expression: "0 0 31 2 *",
			wantErr:    true,
		},
		{
			name:       "April the thirty-first",
			expression: "0 0 31 4 *",
			wantErr:    true,
		},
		{
			name:       "the thirty-first of a month that has one",
			expression: "0 0 31 1 *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseCron(tt.expression, tt.timezone)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrBadCron)
				assert.Nil(t, parsed)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.True(t, parsed.Next(time.Now()).After(time.Now()), "a parsed schedule has a next occurrence")
		})
	}
}

// TestCalculateNextRun covers what the panel renders beside a schedule, including a paused one and
// one whose expression stopped parsing.
func TestCalculateNextRun(t *testing.T) {
	from := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule schedulestore.Schedule
		want     time.Time
	}{
		{
			name: "the next hour",
			schedule: schedulestore.Schedule{
				Cron:     "0 * * * *",
				Timezone: "UTC",
				Enabled:  true,
			},
			want: time.Date(2026, time.July, 27, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "tomorrow morning, since today's has passed",
			schedule: schedulestore.Schedule{
				Cron:     "0 9 * * *",
				Timezone: "UTC",
				Enabled:  true,
			},
			want: time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
		},
		{
			name:     "a paused schedule has no next run",
			schedule: schedulestore.Schedule{Cron: "0 * * * *", Timezone: "UTC"},
		},
		{
			name: "nor does one that cannot be parsed",
			schedule: schedulestore.Schedule{
				Cron:     "0 99 * * *",
				Timezone: "UTC",
				Enabled:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateNextRun(tt.schedule, from)

			if tt.want.IsZero() {
				assert.True(t, got.IsZero())
				return
			}

			assert.Equal(t, tt.want, got.UTC())
		})
	}
}

// TestNewSchedule covers what the service does to a config on the way in, trimming it,
// expanding the launch directory, and refusing an expression that could never fire.
func TestNewSchedule(t *testing.T) {
	tests := []struct {
		name     string
		cfg      schedulestore.Config
		wantErr  error
		wantName string
	}{
		{
			name: "a schedule is stored as written",
			cfg: schedulestore.Config{
				Name:   "morning-ci",
				Cron:   "0 9 * * 1-5",
				Prompt: "check CI",
			},
			wantName: "morning-ci",
		},
		{
			name: "surrounding whitespace is trimmed",
			cfg: schedulestore.Config{
				Name:   "  spaced  ",
				Cron:   "  0 9 * * *  ",
				Prompt: " go ",
			},
			wantName: "spaced",
		},
		{
			name: "an unparseable expression never reaches the store",
			cfg: schedulestore.Config{
				Name:   "broken",
				Cron:   "0 99 * * *",
				Prompt: "go",
			},
			wantErr: ErrBadCron,
		},
		{
			name: "nor does an unknown timezone",
			cfg: schedulestore.Config{
				Name:     "elsewhere",
				Cron:     "0 9 * * *",
				Timezone: "Mars/Olympus",
				Prompt:   "go",
			},
			wantErr: ErrBadCron,
		},
		{
			name: "nor does a date the calendar never reaches",
			cfg: schedulestore.Config{
				Name:   "never",
				Cron:   "0 0 30 2 *",
				Prompt: "go",
			},
			wantErr: ErrBadCron,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

			workflowID := st.SeedWorkflow(t, workflowstore.Config{Name: "backend"}).ID
			cfg := tt.cfg
			cfg.WorkflowID = workflowID

			created, err := svc.NewSchedule(t.Context(), cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				listed, listErr := st.Schedules.ListWatched(t.Context(), nil)
				require.NoError(t, listErr)
				assert.Empty(t, listed, "a refused schedule is not written")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, tt.wantName, created.Name)
			assert.Equal(t, workflowID, created.WorkflowID, "a schedule always names the workflow it launches into")
		})
	}
}

// TestUpdateSchedule pins what an update writes over the schedule it names.
func TestUpdateSchedule(t *testing.T) {
	tests := []struct {
		name     string
		cfg      schedulestore.Config
		wantErr  error
		wantName string
		wantCron string
	}{
		{
			name: "an unreadable expression leaves the schedule as it was",
			cfg: schedulestore.Config{
				Name:   "renamed",
				Cron:   "0 99 * * *",
				Prompt: "go",
			},
			wantErr:  ErrBadCron,
			wantName: "nightly",
			wantCron: "0 9 * * *",
		},
		{
			name: "a blank name keeps the one it has",
			cfg: schedulestore.Config{
				Name:   "  ",
				Cron:   "0 17 * * 5",
				Prompt: "weekly",
			},
			wantName: "nightly",
			wantCron: "0 17 * * 5",
		},
		{
			name: "the fields given replace the ones stored",
			cfg: schedulestore.Config{
				Name:   "weekly",
				Cron:   "0 17 * * 5",
				Prompt: "weekly",
			},
			wantName: "weekly",
			wantCron: "0 17 * * 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

			existing := st.SeedSchedule(t, schedulestore.Config{
				Name:   "nightly",
				Cron:   "0 9 * * *",
				Prompt: "go",
			})

			cfg := tt.cfg
			cfg.WorkflowID = existing.WorkflowID
			err := svc.UpdateSchedule(ctx, existing.ID, cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			got, err := svc.GetSchedule(ctx, existing.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantCron, got.Cron)
		})
	}
}

// TestScheduleWorkflowMustExist pins that a schedule pointing at a missing workflow is
// refused when it is written rather than every time it fires.
func TestScheduleWorkflowMustExist(t *testing.T) {
	tests := []struct {
		name            string
		archiveWorkflow bool
		workflowID      string
		wantErr         error
		wantSaying      string
	}{
		{
			name:            "a workflow that has been archived",
			archiveWorkflow: true,
			wantErr:         ErrWorkflowArchived,
			wantSaying:      "gone",
		},
		{
			name:       "a well-formed id no workflow holds",
			workflowID: missingWorkflowID,
			wantErr:    ErrWorkflowNotFound,
		},
		{
			name:       "an id that is not an id at all",
			workflowID: "not-a-uuid",
			wantErr:    ErrWorkflowNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

			live := st.SeedWorkflow(t, workflowstore.Config{Name: "backend"}).ID
			existing, err := svc.NewSchedule(ctx, schedulestore.Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: live,
			})
			require.NoError(t, err)

			target := tt.workflowID
			if tt.archiveWorkflow {
				target = st.SeedWorkflow(t, workflowstore.Config{Name: "gone"}).ID
				require.NoError(t, st.Workflows.Archive(ctx, target))
			}

			_, err = svc.NewSchedule(ctx, schedulestore.Config{
				Name:       "morning",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: target,
			})
			require.ErrorIs(t, err, tt.wantErr, "a schedule is not created into a workflow that is not there")
			if tt.wantSaying != "" {
				assert.Contains(t, err.Error(), tt.wantSaying, "the refusal names the workflow")
			}

			err = svc.UpdateSchedule(ctx, existing.ID, schedulestore.Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: target,
			})
			require.ErrorIs(t, err, tt.wantErr, "nor is one moved into it")

			listed, err := st.Schedules.ListWatched(ctx, nil)
			require.NoError(t, err)
			require.Len(t, listed, 1, "only the schedule that named a live workflow was written")
			assert.Equal(t, live, listed[0].WorkflowID, "the refused update left the schedule where it was")
		})
	}
}

// TestOrchestratorSchedule pins which agents an orchestrator schedule may mail, and that it is
// filed under its orchestrator's workflow.
func TestOrchestratorSchedule(t *testing.T) {
	tests := []struct {
		name       string
		role       agentstore.Role
		archived   bool
		missing    bool
		prompt     string
		wantErr    error
		wantSaying string
	}{
		{
			name: "a live orchestrator",
			role: agentstore.RoleOrchestrator,
		},
		{
			name:       "an archived orchestrator",
			role:       agentstore.RoleOrchestrator,
			archived:   true,
			wantErr:    ErrOrchestratorArchived,
			wantSaying: "lead",
		},
		{
			name:    "an id no agent holds",
			missing: true,
			wantErr: ErrOrchestratorNotFound,
		},
		{
			name:       "an agent that is not an orchestrator",
			role:       agentstore.RoleImplementer,
			wantErr:    ErrNotAnOrchestrator,
			wantSaying: "implementer",
		},
		{
			name:    "a prompt longer than mail holds",
			role:    agentstore.RoleOrchestrator,
			prompt:  strings.Repeat("x", mail.MaxMailBody+1),
			wantErr: ErrPromptTooLong,
		},
		{
			name:   "a prompt whose characters fit mail though its bytes do not",
			role:   agentstore.RoleOrchestrator,
			prompt: strings.Repeat("é", mail.MaxMailBody),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows, Agents: st.Agents})

			home := st.SeedWorkflow(t, workflowstore.Config{Name: "home"}).ID
			elsewhere := st.SeedWorkflow(t, workflowstore.Config{Name: "elsewhere"}).ID
			agent := st.SeedAgent(t, agentstore.Agent{Name: "lead", Role: tt.role, WorkflowID: home})
			if tt.archived {
				_, err := st.Agents.Archive(ctx, []string{agent.ID})
				require.NoError(t, err)
			}
			target := agent.ID
			if tt.missing {
				target = missingWorkflowID
			}
			prompt := tt.prompt
			if prompt == "" {
				prompt = "check CI"
			}

			cfg := schedulestore.Config{
				Name:                "nightly",
				Cron:                "0 9 * * *",
				Prompt:              prompt,
				HideAgents:          true,
				WorkflowID:          elsewhere,
				Kind:                schedulestore.KindOrchestrator,
				OrchestratorAgentID: &target,
			}

			created, err := svc.NewSchedule(ctx, cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				if tt.wantSaying != "" {
					assert.Contains(t, err.Error(), tt.wantSaying)
				}

				existing := st.SeedSchedule(t, schedulestore.Config{Name: "existing", Cron: "0 9 * * *", Prompt: "go"})
				require.ErrorIs(t, svc.UpdateSchedule(ctx, existing.ID, cfg), tt.wantErr, "nor is one turned into it")

				return
			}
			require.NoError(t, err)

			got, err := svc.GetSchedule(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, schedulestore.KindOrchestrator, got.Kind)
			assert.Equal(t, &agent.ID, got.OrchestratorAgentID)
			assert.Equal(t, home, got.WorkflowID, "it is filed under its orchestrator's workflow rather than the one given")
			assert.False(t, got.HideAgents, "it launches nothing to hide")

			require.NoError(t, svc.UpdateSchedule(ctx, created.ID, schedulestore.Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: elsewhere,
				Kind:       schedulestore.KindImplementer,
			}))
			got, err = svc.GetSchedule(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, schedulestore.KindImplementer, got.Kind, "an update may turn it back into an implementer schedule")
			assert.Nil(t, got.OrchestratorAgentID)
			assert.Equal(t, elsewhere, got.WorkflowID)
		})
	}
}

const missingWorkflowID = "00000000-0000-0000-0000-0000000000ff"

// TestRestoreSchedule pins which archived schedules come back and which are refused while what
// they fire at is gone.
func TestRestoreSchedule(t *testing.T) {
	tests := []struct {
		name                string
		live                bool
		orchestrator        bool
		archiveWorkflow     bool
		archiveOrchestrator bool
		wantErr             error
	}{
		{
			name: "an archived schedule comes back",
		},
		{
			name:         "an archived orchestrator schedule comes back",
			orchestrator: true,
		},
		{
			name:    "a live schedule is not in the archive",
			live:    true,
			wantErr: ErrScheduleNotFound,
		},
		{
			name:            "its workflow has been archived since",
			archiveWorkflow: true,
			wantErr:         ErrWorkflowArchived,
		},
		{
			name:                "its orchestrator has been archived since",
			orchestrator:        true,
			archiveOrchestrator: true,
			wantErr:             ErrOrchestratorArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows, Agents: st.Agents})

			workflow := st.SeedWorkflow(t, workflowstore.Config{Name: "backend"}).ID
			cfg := schedulestore.Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: workflow,
			}
			var lead *agentstore.Agent
			if tt.orchestrator {
				lead = st.SeedAgent(t, agentstore.Agent{Name: "lead", Role: agentstore.RoleOrchestrator, WorkflowID: workflow})
				cfg.Kind = schedulestore.KindOrchestrator
				cfg.OrchestratorAgentID = &lead.ID
			}
			created, err := svc.NewSchedule(ctx, cfg)
			require.NoError(t, err)
			if !tt.live {
				require.NoError(t, svc.ArchiveSchedule(ctx, created.ID))
			}
			if tt.archiveOrchestrator {
				_, err := st.Agents.Archive(ctx, []string{lead.ID})
				require.NoError(t, err)
			}
			if tt.archiveWorkflow {
				require.NoError(t, st.Workflows.Archive(ctx, workflow))
			}

			err = svc.RestoreSchedule(ctx, created.ID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			got, err := svc.GetSchedule(ctx, created.ID)
			require.NoError(t, err)
			assert.Nil(t, got.ArchivedAt)
		})
	}
}

// TestDeleteSchedule pins that only an archived schedule is deleted.
func TestDeleteSchedule(t *testing.T) {
	tests := []struct {
		name    string
		live    bool
		wantErr error
	}{
		{
			name: "an archived schedule is deleted",
		},
		{
			name:    "a live schedule is refused",
			live:    true,
			wantErr: ErrScheduleNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

			created := st.SeedSchedule(t, schedulestore.Config{Name: "nightly", Cron: "0 9 * * *", Prompt: "go"})
			if !tt.live {
				require.NoError(t, svc.ArchiveSchedule(ctx, created.ID))
			}

			err := svc.DeleteSchedule(ctx, created.ID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				_, err = st.Schedules.Get(ctx, created.ID)
				require.NoError(t, err, "the refused schedule is still there")

				return
			}
			require.NoError(t, err)

			_, err = st.Schedules.GetIncludingArchived(ctx, created.ID)
			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

// TestScheduleNotFound pins that an id nothing holds is not-found rather than a silent
// success, so a caller editing a deleted schedule hears about it.
func TestScheduleNotFound(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T, svc *ScheduleService) error
	}{
		{
			name: "updating",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.UpdateSchedule(t.Context(), missingScheduleID,
					schedulestore.Config{
						Name:       "x",
						Cron:       "0 9 * * *",
						Prompt:     "go",
						WorkflowID: "w1",
					})
			},
		},
		{
			name: "pausing",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.SetScheduleEnabled(t.Context(), missingScheduleID, false)
			},
		},
		{
			name: "archiving",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.ArchiveSchedule(t.Context(), missingScheduleID)
			},
		},
		{
			name: "restoring",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.RestoreSchedule(t.Context(), missingScheduleID)
			},
		},
		{
			name: "deleting for good",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.DeleteSchedule(t.Context(), missingScheduleID)
			},
		},
		{
			name: "listing its runs",
			call: func(t *testing.T, svc *ScheduleService) error {
				_, err := svc.ListScheduleRuns(t.Context(), ListScheduleRunsParams{
					ScheduleID: missingScheduleID,
					Limit:      20,
				})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

			require.ErrorIs(t, tt.call(t, svc), ErrScheduleNotFound)
		})
	}
}

const missingScheduleID = "00000000-0000-0000-0000-000000000000"
