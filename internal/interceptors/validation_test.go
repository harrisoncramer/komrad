package interceptors

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestValidationRefusesControlCharacters covers the request fields that end up as a tmux
// window name. A schedule's name is handed to a scheduled launch as the agent's title
// without passing through the agent RPC, so the rule has to sit on the schedule messages
// too or the schedule is simply the way around it.
func TestValidationRefusesControlCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     proto.Message
		wantErr bool
	}{
		{
			name: "an agent title carrying a tab is refused",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "go",
				Title:      "nightly\treview",
				WorkflowId: "w1",
			},
			wantErr: true,
		},
		{
			name: "a new schedule named with a tab is refused",
			req: &schedulev1.CreateScheduleRequest{
				Name:       "nightly\treview",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			},
			wantErr: true,
		},
		{
			name: "a new schedule named with a bell is refused",
			req: &schedulev1.CreateScheduleRequest{
				Name:       "nightly\areview",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			},
			wantErr: true,
		},
		{
			name: "renaming a schedule to a name carrying a newline is refused",
			req: &schedulev1.UpdateScheduleRequest{
				Id:         "s1",
				Name:       "nightly\nreview",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			},
			wantErr: true,
		},
		{
			name: "an ordinary schedule name is accepted",
			req: &schedulev1.CreateScheduleRequest{
				Name:       "nightly review",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			},
		},
	}

	validator, err := protovalidate.New()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.Validate(tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}
