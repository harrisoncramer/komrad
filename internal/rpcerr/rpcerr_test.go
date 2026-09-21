package rpcerr_test

import (
	"errors"
	"fmt"
	"testing"

	"weave/internal/agents"
	"weave/internal/git"
	"weave/internal/rpcerr"
	"weave/internal/scripts"
	"weave/internal/store"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// answered is what a caller is served when a handler returns this failure.
func answered(err error, failure string) error {
	_, served := rpcerr.Answer[emptypb.Empty](nil, rpcerr.From(err, failure))

	return served
}

// TestWrapCodes pins the code every sentinel a handler can receive is answered with.
func TestWrapCodes(t *testing.T) {
	tests := []struct {
		sentinel error
		want     connect.Code
	}{
		{store.ErrNotFound, connect.CodeNotFound},
		{agents.ErrAgentNotFound, connect.CodeNotFound},
		{agents.ErrWorkflowNotFound, connect.CodeNotFound},
		{agents.ErrScheduleNotFound, connect.CodeNotFound},
		{agents.ErrScriptNotFound, connect.CodeNotFound},
		{agents.ErrMailNotFound, connect.CodeNotFound},
		{agents.ErrRecipientNotFound, connect.CodeNotFound},
		{agents.ErrReviewThreadNotFound, connect.CodeNotFound},
		{agents.ErrReviewMessageNotFound, connect.CodeNotFound},
		{agents.ErrParentNotFound, connect.CodeNotFound},
		{store.ErrNameTaken, connect.CodeAlreadyExists},
		{agents.ErrAdoptAlreadyAdopted, connect.CodeAlreadyExists},
		{agents.ErrAlreadyLaunched, connect.CodeAlreadyExists},
		{agents.ErrAgentArchived, connect.CodeFailedPrecondition},
		{agents.ErrProvisionFailed, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotExecutable, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotStartable, connect.CodeFailedPrecondition},
		{agents.ErrScriptDirMissing, connect.CodeFailedPrecondition},
		{agents.ErrScriptAlreadyRunning, connect.CodeFailedPrecondition},
		{agents.ErrWorkflowHasSchedules, connect.CodeFailedPrecondition},
		{agents.ErrScheduleHasAgents, connect.CodeFailedPrecondition},
		{agents.ErrAdoptNeedsWorktree, connect.CodeFailedPrecondition},
		{agents.ErrAdoptBranchInUse, connect.CodeFailedPrecondition},
		{agents.ErrParentStillArchived, connect.CodeFailedPrecondition},
		{agents.ErrChildNeedsParentDirectory, connect.CodeFailedPrecondition},
		{agents.ErrReviewNoRound, connect.CodeFailedPrecondition},
		{agents.ErrRebaseShallow, connect.CodeFailedPrecondition},
		{agents.ErrRebaseHiddenPaths, connect.CodeFailedPrecondition},
		{agents.ErrHookFailed, connect.CodeFailedPrecondition},
		{agents.ErrNoLiveReviewer, connect.CodeFailedPrecondition},
		{git.ErrBaseUnresolved, connect.CodeFailedPrecondition},
		{git.ErrBaseUnrelated, connect.CodeFailedPrecondition},
		{agents.ErrReviewEmpty, connect.CodeFailedPrecondition},
		{agents.ErrReviewAlreadyGiven, connect.CodeFailedPrecondition},
		{agents.ErrBadCron, connect.CodeInvalidArgument},
		{agents.ErrUnknownHookEvent, connect.CodeInvalidArgument},
		{agents.ErrNoticeKindMissing, connect.CodeInvalidArgument},
		{agents.ErrBadDirectory, connect.CodeInvalidArgument},
		{agents.ErrAdoptNotABranch, connect.CodeInvalidArgument},
		{agents.ErrHarnessUnknown, connect.CodeInvalidArgument},
		{agents.ErrSpawnerUnknown, connect.CodeInvalidArgument},
		{agents.ErrParentIsChild, connect.CodeInvalidArgument},
		{agents.ErrChildOwnsLaunch, connect.CodeInvalidArgument},
		{agents.ErrSelfAddressed, connect.CodeInvalidArgument},
		{agents.ErrRecipientAmbiguous, connect.CodeInvalidArgument},
		{agents.ErrSenderUnknown, connect.CodeInvalidArgument},
		{agents.ErrReplyToNotFound, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsCwd, connect.CodeInvalidArgument},
		{agents.ErrWorktreeCwdNotPrimaryCheckout, connect.CodeInvalidArgument},
		{agents.ErrWorktreeUnsafePath, connect.CodeInvalidArgument},
		{agents.ErrTooManyAgents, connect.CodeResourceExhausted},
		{agents.ErrMailboxFull, connect.CodeResourceExhausted},
		{agents.ErrExchangeCapped, connect.CodeResourceExhausted},
		{agents.ErrTooManyChildren, connect.CodeResourceExhausted},
		{agentstore.ErrTooManyMetadataKeys, connect.CodeResourceExhausted},
		{agents.ErrReviewNotYours, connect.CodePermissionDenied},
		{agents.ErrReviewHumanOnly, connect.CodePermissionDenied},
		{agents.ErrReviewNotEntitled, connect.CodePermissionDenied},
		{agents.ErrReviewOwnWork, connect.CodePermissionDenied},
		{agents.ErrOutsideFamily, connect.CodePermissionDenied},
		{agents.ErrOutsideWorkflow, connect.CodePermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.sentinel.Error(), func(t *testing.T) {
			bare := answered(tt.sentinel, "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(bare))

			wrapped := answered(fmt.Errorf("%w: %q", tt.sentinel, "abc"), "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(wrapped), "a sentinel is still found under a wrapping")
			assert.Contains(t, wrapped.Error(), `"abc"`, "and what the domain added to it is kept")
			assert.NotContains(t, wrapped.Error(), "failed to do the thing",
				"the handler's own description is for the failures it cannot name")
		})
	}
}

// TestFromNil pins that converting no error is no failure, so a handler may convert whatever
// it holds without asking first.
func TestFromNil(t *testing.T) {
	assert.Nil(t, rpcerr.From(nil, "failed to do the thing"))
}

// TestWrapCarriesTwoSentinels covers an error holding two sentinels at once, where the order they were wrapped in decides the code.
func TestWrapCarriesTwoSentinels(t *testing.T) {
	err := answered(fmt.Errorf("%w: %w", agents.ErrProvisionFailed, agents.ErrBadDirectory),
		"failed to create agent",
	)

	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err),
		"the provisioner refusing is what the caller can act on")
}

// TestWrapUnknown covers the error a caller cannot fix.
func TestWrapUnknown(t *testing.T) {
	err := answered(errors.New("connection refused"), "failed to archive agents")

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "failed to archive agents")
	assert.Contains(t, err.Error(), "connection refused")
}

// TestWrapSaying covers a sentinel worded for the request that raised it, which keeps its kind
// and replaces its own message.
func TestWrapSaying(t *testing.T) {
	err := answered(store.ErrNotFound.Saying("no schedule with id %q", "s-1"), "failed to get schedule")

	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	assert.Contains(t, err.Error(), `no schedule with id "s-1"`)
	assert.NotContains(t, err.Error(), "not found:", "the sentinel's own message is replaced, not appended")
}

// TestWrapSayingUnderAWrapping covers a worded sentinel the domain put its own prefix on, where
// the caller is told the sentence the raise site chose and nothing else.
func TestWrapSayingUnderAWrapping(t *testing.T) {
	said := store.ErrNotFound.Saying("no schedule with id %q", "s-1")

	err := answered(fmt.Errorf("failed to get schedule %q: %w", "s-1", said), "failed to get schedule")

	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	assert.Equal(t, `not_found: no schedule with id "s-1"`, err.Error())
}

// TestWrapKeepsTheMappedCodeBesideAnUnmappedSentinel covers an error carrying a plain error
// beside a sentinel, which must not cost it the sentinel's code.
func TestWrapKeepsTheMappedCodeBesideAnUnmappedSentinel(t *testing.T) {
	ran := fmt.Errorf("%w: %w: exited 1", agents.ErrProvisionFailed, scripts.ErrFailed)

	err := answered(ran, "failed to create the agent")

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}
