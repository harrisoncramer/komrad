package eventprompts

import (
	"testing"

	eventv1 "komrad/proto/gen/event/v1"

	"github.com/stretchr/testify/assert"
)

// TestRebaseMailStartsTheEventForItsOutcome pins the event each rebase outcome starts.
func TestRebaseMailStartsTheEventForItsOutcome(t *testing.T) {
	tests := []struct {
		name string
		mail Mail
		want eventv1.Event
	}{
		{
			name: "a rebase stopped on conflicts",
			mail: Rebased{Conflicted: true},
			want: eventv1.Event_EVENT_WORKTREE_REBASE_CONFLICTED,
		},
		{
			name: "a branch whose work had all landed",
			mail: Rebased{Landed: 2, Moved: true},
			want: eventv1.Event_EVENT_WORKTREE_LANDED,
		},
		{
			name: "a replay",
			mail: Rebased{Replay: 1},
			want: eventv1.Event_EVENT_WORKTREE_REBASED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, []eventv1.Event{tt.want}, tt.mail.Events())
		})
	}
}

// TestRebaseMailSaysWhatHappenedToTheBranch covers what the rebase mail tells the agent.
func TestRebaseMailSaysWhatHappenedToTheBranch(t *testing.T) {
	const park = "refs/komrad/prerefresh/a1/1"

	tests := []struct {
		name        string
		mail        Rebased
		wantSaid    string
		wantNotSaid string
	}{
		{
			name:     "a rebase stopped on conflicts",
			mail:     Rebased{Conflicted: true, ParkedRef: park},
			wantSaid: park,
		},
		{
			name:        "an agent with no work of its own",
			mail:        Rebased{ParkedRef: park},
			wantSaid:    "You had no commits of your own to replay",
			wantNotSaid: "of your commits are no longer on the branch",
		},
		{
			name: "a branch that reached the base without replaying anything",
			mail: Rebased{
				Landed:    2,
				Moved:     true,
				ParkedRef: park,
			},
			wantSaid: park,
		},
		{
			name:        "a replay that dropped commits the base already held, with no park to name",
			mail:        Rebased{Landed: 2, Replay: 1},
			wantSaid:    "2 of your commits are no longer on the branch",
			wantNotSaid: "A copy of the worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.mail.Body()

			assert.Contains(t, body, tt.wantSaid)
			if tt.wantNotSaid != "" {
				assert.NotContains(t, body, tt.wantNotSaid)
			}
		})
	}
}
