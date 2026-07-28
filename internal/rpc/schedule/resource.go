package schedule

import (
	"time"

	"claude-agents/internal/agents"
	schedulestore "claude-agents/internal/store/schedule"
	schedulev1 "claude-agents/proto/gen/schedule/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Every resource the service lists is paged the same way. The handler renders
// stored rows into the type below, pairing the proto message the caller sees
// with the cursor locating it, and hands the slice to a paginator. The cursor
// lives beside the proto rather than inside it, since it is the server's
// bookkeeping rather than part of the resource.

// schedule is a rendered schedule, the proto sent to the caller plus the cursor
// that finds it again in the listing.
type schedule struct {
	*schedulev1.Schedule
	cursor schedulestore.Cursor
}

// Cursor returns the keyset cursor locating this schedule.
func (s schedule) Cursor() (schedulestore.Cursor, error) {
	return s.cursor, nil
}

// renderSchedule renders a stored schedule into a schedule resource. The next
// run is derived from the cron expression at render time rather than stored, so
// it cannot go stale, and it is left unset for a paused schedule since that one
// has no next run.
func renderSchedule(s schedulestore.Schedule, now time.Time) schedule {
	rendered := &schedulev1.Schedule{
		Id:         s.ID,
		Name:       s.Name,
		Cron:       s.Cron,
		Timezone:   s.Timezone,
		Enabled:    s.Enabled,
		HideAgents: s.HideAgents,
		Prompt:     s.Prompt,
		WorkflowId: s.WorkflowID,
	}

	if next := agents.NextRun(s, now); !next.IsZero() {
		rendered.NextRunAt = timestamppb.New(next)
	}

	return schedule{
		Schedule: rendered,
		cursor:   schedulestore.Cursor{ID: s.ID},
	}
}
