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

// TestWakeLoop pins which waker each named loop reaches and the code an unknown loop gets.
func TestWakeLoop(t *testing.T) {
	tests := []struct {
		name     string
		loop     loopv1.Loop
		wantCode connect.Code
		want     []int
	}{
		{
			name:     "a loop that names none",
			loop:     loopv1.Loop_LOOP_UNSPECIFIED,
			wantCode: connect.CodeInvalidArgument,
			want:     []int{0, 0, 0},
		},
		{
			name: "the cwd sync",
			loop: loopv1.Loop_LOOP_CWD_SYNC,
			want: []int{1, 0, 0},
		},
		{
			name: "the transcript sync",
			loop: loopv1.Loop_LOOP_TRANSCRIPT_SYNC,
			want: []int{0, 1, 0},
		},
		{
			name: "the pull request sync",
			loop: loopv1.Loop_LOOP_PULL_REQUEST_SYNC,
			want: []int{0, 0, 1},
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

			assert.Equal(t, tt.want, []int{cwd.Wakes, transcripts.Wakes, pullRequests.Wakes})
		})
	}
}
