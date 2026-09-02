package action

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/agents"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestOpenPullRequestCodes covers the codes telling an agent that answers to nothing apart from
// a built-in script that is there and will not run.
func TestOpenPullRequestCodes(t *testing.T) {
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
			err:      agents.ErrActionNotExecutable,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "a shebang naming an interpreter that is not there",
			err:      agents.ErrActionNotStartable,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "an agent whose directory has been removed from under it",
			err:      agents.ErrActionDirMissing,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "the same action already running for the same agent",
			err:      agents.ErrActionAlreadyRunning,
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

			_, err := service.OpenPullRequest(context.Background(), connect.NewRequest(&actionv1.OpenPullRequestRequest{
				AgentId: "a1",
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

// TestOpenPullRequestPassesWhatItWasAsked covers the handler translating and nothing more.
func TestOpenPullRequestPassesWhatItWasAsked(t *testing.T) {
	manager := &fakeActions{}
	service := NewService(manager)

	_, err := service.OpenPullRequest(context.Background(), connect.NewRequest(&actionv1.OpenPullRequestRequest{
		AgentId: "a7",
	}))

	require.NoError(t, err)
	assert.Equal(t, "a7", manager.agentID)
}

// TestListHooks covers a hook with no declared timeout leaving the field unset, since a zero
// would read as a run with no time at all.
func TestListHooks(t *testing.T) {
	manager := &fakeActions{listedHooks: []agents.Action{
		{
			Name: "install-deps", Path: "/hooks/install-deps", Executable: true,
			Header: agents.ActionHeader{
				Description: "Install what the worktree needs before the agent starts.",
				Timeout:     45 * time.Second,
				Problems:    []string{"line 4: nope is not a directive weave knows"},
			},
		},
		{
			Name:       "compiled",
			Path:       "/hooks/compiled",
			Executable: true,
		},
	}}

	resp, err := NewService(manager).ListHooks(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	listed := resp.Msg.GetHooks()
	require.Len(t, listed, 2)

	assert.Equal(t, "install-deps", listed[0].GetName())
	assert.True(t, listed[0].GetExecutable())
	assert.Equal(t, "Install what the worktree needs before the agent starts.", listed[0].GetDescription())
	assert.Equal(t, 45*time.Second, listed[0].GetTimeout().AsDuration())
	assert.Equal(t, []string{"line 4: nope is not a directive weave knows"}, listed[0].GetHeaderProblems())

	assert.Empty(t, listed[1].GetDescription())
	assert.Nil(t, listed[1].GetTimeout(), "a file that declared no timeout sends none")
}

// TestListHooksFailure covers a directory that could not be read at all, which is the server's
// problem rather than the caller's.
func TestListHooksFailure(t *testing.T) {
	service := NewService(&fakeActions{listErr: errors.New("permission denied")})

	_, err := service.ListHooks(context.Background(), connect.NewRequest(&emptypb.Empty{}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

type fakeActions struct {
	listedHooks []agents.Action
	listErr     error
	err         error
	agentID     string
}

func (f *fakeActions) ListHooks() ([]agents.Action, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.listedHooks, nil
}

func (f *fakeActions) OpenPullRequest(_ context.Context, agentID string) error {
	f.agentID = agentID

	return f.err
}
