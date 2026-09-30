package pullrequests

import (
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDismissForgeThread covers who may hide a forge thread from the review.
func TestDismissForgeThread(t *testing.T) {
	tests := []struct {
		name      string
		agentID   string
		wantErr   error
		dismissed bool
	}{
		{
			name:      "the person hides it",
			dismissed: true,
		},
		{
			name:    "an agent is refused and the thread stays",
			agentID: "01a0f025-00bd-7db3-90c0-a0198677e92e",
			wantErr: ErrDismissHumanOnly,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommentFixture(t)
			f.forge.threads[1] = &forge.Threads{Threads: []forge.ReviewThread{lineThread("T_1", "dana")}}
			f.commentPass(t, syncNow)
			threads := f.stored(t)
			require.Len(t, threads, 1)

			dismisser := NewDismisser(NewDismisserParams{Threads: f.threads})
			err := dismisser.DismissForgeThread(t.Context(), DismissForgeThreadParams{ID: threads[0].ID, AgentID: tt.agentID})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.dismissed, f.stored(t)[0].Dismissed)
		})
	}
}
