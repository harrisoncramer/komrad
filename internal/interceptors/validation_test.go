package interceptors

import (
	"context"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidationRefusesControlCharacters covers the fields that become a tmux window name,
// where a firing hands a schedule's name straight to the agent service as the title.
func TestValidationRefusesControlCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     connect.AnyRequest
		wantErr bool
	}{
		{
			name: "an agent title carrying a tab is refused",
			req: connect.NewRequest(&agentv1.CreateAgentRequest{
				Prompt:     "go",
				Title:      "nightly\treview",
				WorkflowId: "w1",
			}),
			wantErr: true,
		},
		{
			name: "a new schedule named with a tab is refused",
			req: connect.NewRequest(&schedulev1.CreateScheduleRequest{
				Name:       "nightly\treview",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			}),
			wantErr: true,
		},
		{
			name: "renaming a schedule to a name carrying a delete is refused",
			req: connect.NewRequest(&schedulev1.UpdateScheduleRequest{
				Id:         "s1",
				Name:       "nightly\x7freview",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			}),
			wantErr: true,
		},
		{
			name: "an ordinary schedule name is accepted",
			req: connect.NewRequest(&schedulev1.CreateScheduleRequest{
				Name:       "nightly review",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowId: "w1",
			}),
		},
	}

	interceptor, err := NewValidation()
	require.NoError(t, err)

	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&schedulev1.CreateScheduleResponse{}), nil
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := interceptor.WrapUnary(next)(t.Context(), tt.req)
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}

			assert.NoError(t, err)
		})
	}
}
