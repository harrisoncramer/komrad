package schedules

import (
	"context"
	"errors"
	"testing"

	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPeriodic struct {
	events  []string
	refused bool
}

func (r *recordingPeriodic) Add(_ string, _ river.PeriodicSchedule) error {
	r.events = append(r.events, "add")

	if r.refused {
		return errors.New("refusing to register")
	}

	return nil
}

func (r *recordingPeriodic) Remove(_ string) {
	r.events = append(r.events, "remove")
}

type scheduleEdit func(ctx context.Context, svc *ScheduleService, id string, cfg schedulestore.Config) error

// TestScheduleWritesKeepRegistrations pins what each schedule write does to its River registration.
func TestScheduleWritesKeepRegistrations(t *testing.T) {
	tests := []struct {
		name       string
		paused     bool
		refused    bool
		edits      []scheduleEdit
		wantErr    bool
		wantEvents []string
	}{
		{
			name:       "a registration River refuses fails the write",
			refused:    true,
			wantErr:    true,
			wantEvents: []string{"add"},
		},
		{
			name:   "a paused schedule is not registered",
			paused: true,
		},
		{
			name:   "nor is one restored while paused",
			paused: true,
			edits:  []scheduleEdit{archiveEdit, restoreEdit},
		},
		{
			name:       "an armed schedule is registered",
			wantEvents: []string{"add"},
		},
		{
			name:       "pausing takes the registration off",
			edits:      []scheduleEdit{newEnabledEdit(false)},
			wantEvents: []string{"add", "remove"},
		},
		{
			name:       "resuming puts it back",
			edits:      []scheduleEdit{newEnabledEdit(false), newEnabledEdit(true)},
			wantEvents: []string{"add", "remove", "add"},
		},
		{
			name:       "resuming a schedule that is already armed registers nothing again",
			edits:      []scheduleEdit{newEnabledEdit(true)},
			wantEvents: []string{"add"},
		},
		{
			name:       "a changed expression replaces the registration",
			edits:      []scheduleEdit{newCronEdit("*/5 * * * *", "")},
			wantEvents: []string{"add", "remove", "add"},
		},
		{
			name:       "a changed timezone counts as a change too",
			edits:      []scheduleEdit{newCronEdit("0 9 * * *", "America/New_York")},
			wantEvents: []string{"add", "remove", "add"},
		},
		{
			name:       "renaming leaves the registration alone",
			edits:      []scheduleEdit{renameEdit},
			wantEvents: []string{"add"},
		},
		{
			name:       "archiving takes the registration off",
			edits:      []scheduleEdit{archiveEdit},
			wantEvents: []string{"add", "remove"},
		},
		{
			name:       "restoring puts it back",
			edits:      []scheduleEdit{archiveEdit, restoreEdit},
			wantEvents: []string{"add", "remove", "add"},
		},
		{
			name:       "deleting an archived schedule touches nothing more",
			edits:      []scheduleEdit{archiveEdit, deleteEdit},
			wantEvents: []string{"add", "remove"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})
			periodic := &recordingPeriodic{refused: tt.refused}
			require.NoError(t, svc.ArmPeriodic(ctx, periodic))

			cfg := schedulestore.Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				Enabled:    !tt.paused,
				WorkflowID: st.SeedWorkflow(t, workflowstore.Config{Name: "backend"}).ID,
			}
			created, err := svc.NewSchedule(ctx, cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.wantEvents, periodic.events)

				return
			}
			require.NoError(t, err)

			for _, edit := range tt.edits {
				require.NoError(t, edit(ctx, svc, created.ID, cfg))
			}

			assert.Equal(t, tt.wantEvents, periodic.events)
		})
	}
}

// TestArmPeriodic pins that arming registers only the schedules that can fire.
func TestArmPeriodic(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	svc := NewScheduleService(NewScheduleServiceParams{Schedules: st.Schedules, Workflows: st.Workflows})

	armed := st.SeedSchedule(t, schedulestore.Config{
		Name:    "armed",
		Cron:    "0 9 * * *",
		Prompt:  "go",
		Enabled: true,
	})
	st.SeedSchedule(t, schedulestore.Config{
		Name:   "paused",
		Cron:   "0 9 * * *",
		Prompt: "go",
	})
	archived := st.SeedSchedule(t, schedulestore.Config{
		Name:    "archived",
		Cron:    "0 9 * * *",
		Prompt:  "go",
		Enabled: true,
	})
	require.NoError(t, st.Schedules.Archive(ctx, archived.ID))

	periodic := &recordingPeriodic{}
	require.NoError(t, svc.ArmPeriodic(ctx, periodic))
	assert.Equal(t, []string{"add"}, periodic.events)

	require.NoError(t, svc.SetScheduleEnabled(ctx, armed.ID, true))
	assert.Equal(t, []string{"add"}, periodic.events, "a schedule armed at boot is not registered twice")
}

func newEnabledEdit(enabled bool) scheduleEdit {
	return func(ctx context.Context, svc *ScheduleService, id string, _ schedulestore.Config) error {
		return svc.SetScheduleEnabled(ctx, id, enabled)
	}
}

func newCronEdit(cron, timezone string) scheduleEdit {
	return func(ctx context.Context, svc *ScheduleService, id string, cfg schedulestore.Config) error {
		cfg.Cron = cron
		cfg.Timezone = timezone

		return svc.UpdateSchedule(ctx, id, cfg)
	}
}

func renameEdit(ctx context.Context, svc *ScheduleService, id string, cfg schedulestore.Config) error {
	cfg.Name = "renamed"

	return svc.UpdateSchedule(ctx, id, cfg)
}

func archiveEdit(ctx context.Context, svc *ScheduleService, id string, _ schedulestore.Config) error {
	return svc.ArchiveSchedule(ctx, id)
}

func restoreEdit(ctx context.Context, svc *ScheduleService, id string, _ schedulestore.Config) error {
	return svc.RestoreSchedule(ctx, id)
}

func deleteEdit(ctx context.Context, svc *ScheduleService, id string, _ schedulestore.Config) error {
	return svc.DeleteSchedule(ctx, id)
}
