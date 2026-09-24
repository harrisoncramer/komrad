package interceptors

import (
	"context"
	"testing"

	agentv1 "komrade/proto/gen/agent/v1"
	schedulev1 "komrade/proto/gen/schedule/v1"
	viewv1 "komrade/proto/gen/view/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
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

			require.NoError(t, err)
		})
	}
}

// TestValidationRefusesAnUndeclaredEnumNumber covers a number no enum value declares.
func TestValidationRefusesAnUndeclaredEnumNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     connect.AnyRequest
		wantErr bool
	}{
		{
			name:    "a view nothing declares is refused",
			req:     connect.NewRequest(&agentv1.ListAgentsRequest{View: 99}),
			wantErr: true,
		},
		{
			name:    "a mail direction nothing declares is refused",
			req:     connect.NewRequest(&agentv1.ListMailRequest{Direction: 99}),
			wantErr: true,
		},
		{
			name: "a view the schema declares is accepted",
			req:  connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL}),
		},
	}

	interceptor, err := NewValidation()
	require.NoError(t, err)

	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := interceptor.WrapUnary(next)(t.Context(), tt.req)
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
		})
	}
}

// receivingConn hands a stream's handler one fixed message.
type receivingConn struct {
	connect.StreamingHandlerConn
	message proto.Message
}

func (c receivingConn) Receive(msg any) error {
	proto.Merge(msg.(proto.Message), c.message)

	return nil
}

// TestValidationRefusesAStreamsBadRequest pins the streaming arm: the message a handler reads
// off the stream is held to the same rules a unary request is.
func TestValidationRefusesAStreamsBadRequest(t *testing.T) {
	interceptor, err := NewValidation()
	require.NoError(t, err)

	var received error
	next := func(_ context.Context, conn connect.StreamingHandlerConn) error {
		received = conn.Receive(&agentv1.GetAgentRequest{})

		return nil
	}

	require.NoError(t, interceptor.WrapStreamingHandler(next)(t.Context(), receivingConn{message: &agentv1.GetAgentRequest{}}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(received), "an empty id is refused as it is read")

	require.NoError(t, interceptor.WrapStreamingHandler(next)(t.Context(), receivingConn{message: &agentv1.GetAgentRequest{Id: "a1"}}))
	require.NoError(t, received)
}
