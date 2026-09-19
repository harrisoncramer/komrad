package hook

import (
	"errors"
	"testing"
	"time"

	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"

	"weave/internal/agents"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

	resp, err := hookv1weave.HookServiceAdapter{Impl: NewHookService(manager)}.ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))
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
	service := hookv1weave.HookServiceAdapter{Impl: NewHookService(failing)}

	_, err := service.ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

type fakeHooks struct {
	listedHooks []agents.Script
	listErr     error
}

func (f *fakeHooks) ListHooks() ([]agents.Script, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.listedHooks, nil
}
