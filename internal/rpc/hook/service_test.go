package hook

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/agents"
	hookv1 "weave/proto/gen/hook/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestOpenPullRequest covers the handler translating and nothing more, the code each sentinel earns being rpcerr's own.
func TestOpenPullRequest(t *testing.T) {
	manager := &fakeHooks{}
	service := NewService(manager, manager)

	_, err := service.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
		AgentId: "a7",
	}))

	require.NoError(t, err)
	assert.Equal(t, "a7", manager.agentID)

	refusing := &fakeHooks{err: agents.ErrNoPullRequest}
	refused := NewService(refusing, refusing)

	_, err = refused.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
		AgentId: "a1",
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

// TestListHooks covers a hook with no declared timeout leaving the field unset rather than
// writing a zero.
func TestListHooks(t *testing.T) {
	manager := &fakeHooks{listedHooks: []agents.Script{
		{
			Name: "install-deps", Path: "/hooks/install-deps", Executable: true,
			Header: agents.ScriptHeader{
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

	resp, err := NewService(manager, manager).ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))
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
	failing := &fakeHooks{listErr: errors.New("permission denied")}
	service := NewService(failing, failing)

	_, err := service.ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

type fakeHooks struct {
	listedHooks []agents.Script
	listErr     error
	err         error
	agentID     string
}

func (f *fakeHooks) ListHooks() ([]agents.Script, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.listedHooks, nil
}

func (f *fakeHooks) OpenPullRequest(_ context.Context, agentID string) error {
	f.agentID = agentID

	return f.err
}
