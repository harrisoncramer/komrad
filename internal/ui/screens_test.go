package ui

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryScreenAnswersForItself walks the enum rather than a list of its own, so a screen
// added with an arm that answers for nothing fails here the way one added with no arm at all
// fails the exhaustive check.
func TestEveryScreenAnswersForItself(t *testing.T) {
	for s := screenAgents; s <= screenReview; s++ {
		spec := s.spec()

		require.NotNil(t, spec.key, "screen %d takes no keys", s)
		require.NotNil(t, spec.view, "screen %d draws nothing", s)

		if spec.fromListing {
			assert.NotNil(t, spec.stage, "screen %d is drawn from the listing but stages nothing", s)
		}

		if spec.details != nil {
			assert.NotNil(t, spec.details.hovered, "screen %d draws details of nothing", s)
			assert.NotNil(t, spec.details.quit, "screen %d draws details with no way out", s)
		}

		if spec.filter != nil {
			assert.NotNil(t, spec.filter.hovered, "screen %d filters with no cursor to keep", s)
			assert.NotNil(t, spec.filter.place, "screen %d filters without placing its cursor again", s)
		}
	}
}

// TestFormIsDrawnOverTheScreenItWasOpenedFrom covers the form being chrome rather than a screen
// of its own, since what closing it lands on is whatever it went up over.
func TestFormIsDrawnOverTheScreenItWasOpenedFrom(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())

	opened, _ := m.archiveScheduleConfirm()
	prompt, ok := opened.(model)
	require.True(t, ok)

	require.Equal(t, formScheduleArchive, prompt.form.kind)
	assert.Equal(t, screenSchedules, prompt.screen, "the schedules are still the screen underneath")

	closed, _ := prompt.closeForm()
	got, ok := closed.(model)
	require.True(t, ok)

	assert.False(t, got.form.active(), "closing takes the prompt down")
	assert.Equal(t, screenSchedules, got.screen, "and lands back where it was opened from")
}
