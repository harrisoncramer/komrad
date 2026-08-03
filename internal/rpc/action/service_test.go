package action

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/actions"
	"weave/internal/agents"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestRunActionCodes covers the whole of what a caller can be told. An action is fired and
// forgotten, so every failure is from before the process started, and the code has to say which
// kind it was: a name that answers to nothing, or a file that is there and will not run.
func TestRunActionCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name:     "an agent that is not there",
			err:      agents.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an action the directory does not hold",
			err:      agents.ErrActionNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a file the user has not made executable",
			err:      actions.ErrNotExecutable,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "a shebang naming an interpreter that is not there",
			err:      actions.ErrNotStartable,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			// Nothing about the action is wrong, so this reads the same way a chmod does.
			// An agent outliving the worktree it ran in is how it happens.
			name:     "an agent whose directory has been removed from under it",
			err:      actions.ErrDirMissing,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "anything else is ours rather than theirs",
			err:      errors.New("the config would not parse"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "a started action answers with nothing to say",
			err:      nil,
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(&fakeActions{err: tt.err})

			_, err := service.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
				AgentId: "a1",
				Action:  "open-pr",
			}))

			if tt.wantCode == 0 {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestRunActionPassesWhatItWasAsked covers the handler translating and nothing more. Which agent
// and which action are the caller's to decide, and the service is where they mean anything.
func TestRunActionPassesWhatItWasAsked(t *testing.T) {
	manager := &fakeActions{}
	service := NewService(manager)

	_, err := service.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
		AgentId: "a7",
		Action:  "open-linear",
	}))

	require.NoError(t, err)
	assert.Equal(t, "a7", manager.agentID)
	assert.Equal(t, "open-linear", manager.action)
}

// TestListActions covers the listing rendered onto the wire. An action that declared no timeout
// leaves the field unset rather than sending zero, which a caller would read as a run with no
// time at all.
func TestListActions(t *testing.T) {
	manager := &fakeActions{listed: []actions.Action{
		{
			Name: "open-pr", Path: "/actions/open-pr", Executable: true,
			Header: actions.Header{
				Description: "Open the pull request this agent recorded.",
				Timeout:     45 * time.Second,
				Problems:    []string{"line 4: nope is not a directive weave knows"},
			},
		},
		{Name: "compiled", Path: "/actions/compiled", Executable: true},
	}}

	resp, err := NewService(manager).ListActions(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	listed := resp.Msg.GetActions()
	require.Len(t, listed, 2)

	assert.Equal(t, "open-pr", listed[0].GetName())
	assert.True(t, listed[0].GetExecutable())
	assert.Equal(t, "Open the pull request this agent recorded.", listed[0].GetDescription())
	assert.Equal(t, 45*time.Second, listed[0].GetTimeout().AsDuration())
	assert.Equal(t, []string{"line 4: nope is not a directive weave knows"}, listed[0].GetHeaderProblems())

	assert.Empty(t, listed[1].GetDescription())
	assert.Nil(t, listed[1].GetTimeout(), "a file that declared no timeout sends none")
}

// TestListActionsFailure covers a directory that could not be read at all, which is the server's
// problem rather than the caller's.
func TestListActionsFailure(t *testing.T) {
	service := NewService(&fakeActions{listErr: errors.New("permission denied")})

	_, err := service.ListActions(context.Background(), connect.NewRequest(&emptypb.Empty{}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

// fakeActions stands in for the service the handlers translate for, recording what it was asked
// to fire so a case can assert the handler passed it through unchanged.
type fakeActions struct {
	listed  []actions.Action
	listErr error
	err     error
	agentID string
	action  string
}

func (f *fakeActions) ListActions() ([]actions.Action, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.listed, nil
}

func (f *fakeActions) RunAction(_ context.Context, agentID, name string) error {
	f.agentID, f.action = agentID, name

	return f.err
}
