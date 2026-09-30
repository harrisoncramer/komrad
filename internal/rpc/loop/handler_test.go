package loop

import (
	"testing"

	"komrad/internal/waketest"
	loopv1 "komrad/proto/gen/loop/v1"
	loopv1komrad "komrad/proto/gen/loop/v1/loopv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wakes struct {
	cwd          int
	transcripts  int
	pullRequests int
}

// TestWakeLoop pins which waker each named loop reaches and the code an unknown loop gets.
func TestWakeLoop(t *testing.T) {
	tests := []struct {
		name      string
		loop      loopv1.Loop
		wantCode  connect.Code
		wantWakes wakes
	}{
		{
			name:      "a loop that names none",
			loop:      loopv1.Loop_LOOP_UNSPECIFIED,
			wantCode:  connect.CodeInvalidArgument,
			wantWakes: wakes{},
		},
		{
			name:      "the cwd sync",
			loop:      loopv1.Loop_LOOP_CWD_SYNC,
			wantWakes: wakes{cwd: 1},
		},
		{
			name:      "the transcript sync",
			loop:      loopv1.Loop_LOOP_TRANSCRIPT_SYNC,
			wantWakes: wakes{transcripts: 1},
		},
		{
			name:      "the pull request sync",
			loop:      loopv1.Loop_LOOP_PULL_REQUEST_SYNC,
			wantWakes: wakes{pullRequests: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cwd, transcripts, pullRequests := &waketest.Counter{}, &waketest.Counter{}, &waketest.Counter{}
			adapter := loopv1komrad.LoopServiceAdapter{Impl: NewHandler(NewHandlerParams{
				CwdSync:         cwd,
				TranscriptSync:  transcripts,
				PullRequestSync: pullRequests,
			})}

			_, err := adapter.WakeLoop(t.Context(), connect.NewRequest(&loopv1.WakeLoopRequest{Loop: tt.loop}))
			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantWakes, wakes{
				cwd:          cwd.Wakes,
				transcripts:  transcripts.Wakes,
				pullRequests: pullRequests.Wakes,
			})
		})
	}
}
