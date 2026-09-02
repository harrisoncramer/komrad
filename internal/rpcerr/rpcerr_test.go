package rpcerr_test

import (
	"errors"
	"fmt"
	"testing"

	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/errui"
	"weave/internal/rpcerr"
	"weave/internal/store"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapCodes pins the code every sentinel a handler can receive is answered with, which is
// the mapping the handlers used to each rewrite for themselves.
func TestWrapCodes(t *testing.T) {
	tests := []struct {
		sentinel error
		want     connect.Code
	}{
		{store.ErrNotFound, connect.CodeNotFound},
		{agents.ErrAgentNotFound, connect.CodeNotFound},
		{agents.ErrWorkflowNotFound, connect.CodeNotFound},
		{agents.ErrScheduleNotFound, connect.CodeNotFound},
		{agents.ErrActionNotFound, connect.CodeNotFound},
		{agents.ErrMailNotFound, connect.CodeNotFound},
		{agents.ErrRecipientNotFound, connect.CodeNotFound},
		{agents.ErrReviewThreadNotFound, connect.CodeNotFound},
		{agents.ErrReviewMessageNotFound, connect.CodeNotFound},
		{agents.ErrParentNotFound, connect.CodeNotFound},
		{store.ErrNameTaken, connect.CodeAlreadyExists},
		{agents.ErrAgentArchived, connect.CodeFailedPrecondition},
		{agents.ErrSetupFailed, connect.CodeFailedPrecondition},
		{agents.ErrActionNotExecutable, connect.CodeFailedPrecondition},
		{agents.ErrActionNotStartable, connect.CodeFailedPrecondition},
		{agents.ErrActionDirMissing, connect.CodeFailedPrecondition},
		{agents.ErrActionAlreadyRunning, connect.CodeFailedPrecondition},
		{agents.ErrWorkflowHasSchedules, connect.CodeFailedPrecondition},
		{agents.ErrScheduleHasAgents, connect.CodeFailedPrecondition},
		{agents.ErrAdoptNeedsWorktree, connect.CodeFailedPrecondition},
		{agents.ErrAdoptBranchInUse, connect.CodeFailedPrecondition},
		{agents.ErrChildRidesAlong, connect.CodeFailedPrecondition},
		{agents.ErrChildNeedsParentDirectory, connect.CodeFailedPrecondition},
		{agents.ErrBadCron, connect.CodeInvalidArgument},
		{agents.ErrBadDirectory, connect.CodeInvalidArgument},
		{agents.ErrHarnessUnknown, connect.CodeInvalidArgument},
		{agents.ErrSpawnerUnknown, connect.CodeInvalidArgument},
		{agents.ErrParentIsChild, connect.CodeInvalidArgument},
		{agents.ErrChildOwnsLaunch, connect.CodeInvalidArgument},
		{agents.ErrSelfAddressed, connect.CodeInvalidArgument},
		{agents.ErrSenderUnknown, connect.CodeInvalidArgument},
		{agents.ErrReplyToNotFound, connect.CodeInvalidArgument},
		{agents.ErrAdoptRepoMismatch, connect.CodeInvalidArgument},
		{agents.ErrAdoptForkBranch, connect.CodeInvalidArgument},
		{agents.ErrAdoptNoResolver, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsCwd, connect.CodeInvalidArgument},
		{agents.ErrWorktreeOwnsActions, connect.CodeInvalidArgument},
		{agents.ErrWorktreeActionsReserved, connect.CodeInvalidArgument},
		{agents.ErrWorktreeCwdNotPrimaryCheckout, connect.CodeInvalidArgument},
		{agents.ErrWorktreeUnsafePath, connect.CodeInvalidArgument},
		{agents.ErrTooManyAgents, connect.CodeResourceExhausted},
		{agents.ErrMailboxFull, connect.CodeResourceExhausted},
		{agents.ErrThreadCapped, connect.CodeResourceExhausted},
		{agents.ErrTooManyChildren, connect.CodeResourceExhausted},
		{agentstore.ErrTooManyMetadataKeys, connect.CodeResourceExhausted},
		{agents.ErrReviewNotYours, connect.CodePermissionDenied},
		{agents.ErrReviewHumanOnly, connect.CodePermissionDenied},
		{agents.ErrOutsideFamily, connect.CodePermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.sentinel.Error(), func(t *testing.T) {
			bare := rpcerr.Wrap(tt.sentinel, "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(bare))

			wrapped := rpcerr.Wrap(fmt.Errorf("%w: %q", tt.sentinel, "abc"), "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(wrapped), "a sentinel is still found under a wrapping")
			assert.Contains(t, wrapped.Error(), `"abc"`, "and what the domain added to it is kept")
			assert.NotContains(t, wrapped.Error(), "failed to do the thing",
				"the handler's own description is for the failures it cannot name")
		})
	}
}

// TestWrapNil pins that wrapping no error is no error, so a handler can wrap unconditionally.
func TestWrapNil(t *testing.T) {
	assert.NoError(t, rpcerr.Wrap(nil, "failed to do the thing"))
}

// TestWrapUnknown covers the error a caller cannot fix, which is internal and says only what
// the handler was doing.
func TestWrapUnknown(t *testing.T) {
	err := rpcerr.Wrap(errors.New("connection refused"), "failed to archive agents")

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "failed to archive agents")
	assert.Contains(t, err.Error(), "connection refused")
}

// TestWrapSaying covers a sentinel whose own message says less than the request can.
func TestWrapSaying(t *testing.T) {
	err := rpcerr.Wrap(store.ErrNotFound, "failed to get schedule",
		rpcerr.Saying(store.ErrNotFound, "no schedule with id %q", "s-1"))

	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	assert.Contains(t, err.Error(), `no schedule with id "s-1"`)
	assert.NotContains(t, err.Error(), "not found:", "the sentinel's own message is replaced, not appended")
}

// TestWrapSayingLeavesOthers covers a replacement named for a sentinel the error is not.
func TestWrapSayingLeavesOthers(t *testing.T) {
	err := rpcerr.Wrap(agents.ErrTooManyAgents, "failed to restore agents",
		rpcerr.Saying(store.ErrNameTaken, "the name is taken"))

	require.Error(t, err)
	assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "too many agents")
}

// TestWrapMarksAnActionThatRan covers the failure whose reason is already on screen, which the
// panel is told about so it does not print the output the pane is showing.
func TestWrapMarksAnActionThatRan(t *testing.T) {
	ran := fmt.Errorf("%w: %w: exited 1", agents.ErrSetupFailed, actions.ErrFailed)

	err := rpcerr.Wrap(ran, "failed to create the agent")

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.True(t, errui.SetupRan(err))
}

// TestWrapLeavesRefusalsUnmarked covers a refusal reached before any action ran, which the panel
// has to report itself since nothing else says what happened.
func TestWrapLeavesRefusalsUnmarked(t *testing.T) {
	err := rpcerr.Wrap(agents.ErrAdoptBranchInUse, "failed to adopt")

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.False(t, errui.SetupRan(err))
}
