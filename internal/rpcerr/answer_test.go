package rpcerr_test

import (
	"errors"
	"fmt"
	"testing"

	"komrad/internal/agentdirs"
	"komrad/internal/agents"
	"komrad/internal/checkouts"
	"komrad/internal/git"
	"komrad/internal/mail"
	"komrad/internal/reviews"
	"komrad/internal/rpcerr"
	"komrad/internal/schedules"
	"komrad/internal/scripts"
	"komrad/internal/store"

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
		{schedules.ErrScheduleNotFound, connect.CodeNotFound},
		{agents.ErrScriptNotFound, connect.CodeNotFound},
		{mail.ErrMailNotFound, connect.CodeNotFound},
		{mail.ErrRecipientNotFound, connect.CodeNotFound},
		{reviews.ErrReviewThreadNotFound, connect.CodeNotFound},
		{reviews.ErrReviewMessageNotFound, connect.CodeNotFound},
		{agents.ErrParentNotFound, connect.CodeNotFound},
		{store.ErrNameTaken, connect.CodeAlreadyExists},
		{agents.ErrAlreadyLaunched, connect.CodeAlreadyExists},
		{agents.ErrAgentArchived, connect.CodeFailedPrecondition},
		{agents.ErrProvisionFailed, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotExecutable, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotStartable, connect.CodeFailedPrecondition},
		{agents.ErrScriptDirMissing, connect.CodeFailedPrecondition},
		{agents.ErrScriptAlreadyRunning, connect.CodeFailedPrecondition},
		{agents.ErrWorkflowHasSchedules, connect.CodeFailedPrecondition},
		{agents.ErrAdoptNeedsWorktree, connect.CodeFailedPrecondition},
		{agents.ErrAdoptBranchInUse, connect.CodeFailedPrecondition},
		{agents.ErrParentStillArchived, connect.CodeFailedPrecondition},
		{agents.ErrChildNeedsParentDirectory, connect.CodeFailedPrecondition},
		{reviews.ErrReviewNoRound, connect.CodeFailedPrecondition},
		{checkouts.ErrRebaseShallow, connect.CodeFailedPrecondition},
		{checkouts.ErrRebaseHiddenPaths, connect.CodeFailedPrecondition},
		{agents.ErrHookFailed, connect.CodeFailedPrecondition},
		{agents.ErrNoLiveReviewer, connect.CodeFailedPrecondition},
		{git.ErrBaseMissing, connect.CodeFailedPrecondition},
		{git.ErrBaseUnrelated, connect.CodeFailedPrecondition},
		{reviews.ErrReviewEmpty, connect.CodeFailedPrecondition},
		{reviews.ErrReviewAlreadyGiven, connect.CodeFailedPrecondition},
		{schedules.ErrBadCron, connect.CodeInvalidArgument},
		{agents.ErrUnknownHookEvent, connect.CodeInvalidArgument},
		{agents.ErrUnknownHookScript, connect.CodeInvalidArgument},
		{mail.ErrKindMissing, connect.CodeInvalidArgument},
		{agents.ErrBadDirectory, connect.CodeInvalidArgument},
		{agentdirs.ErrNoDirectory, connect.CodeInvalidArgument},
		{agents.ErrAdoptNotABranch, connect.CodeInvalidArgument},
		{agents.ErrHarnessUnknown, connect.CodeInvalidArgument},
		{agents.ErrSpawnerUnknown, connect.CodeInvalidArgument},
		{agents.ErrParentIsChild, connect.CodeInvalidArgument},
		{agents.ErrChildOwnsLaunch, connect.CodeInvalidArgument},
		{mail.ErrSelfAddressed, connect.CodeInvalidArgument},
		{mail.ErrRecipientAmbiguous, connect.CodeInvalidArgument},
		{mail.ErrSenderUnknown, connect.CodeInvalidArgument},
		{mail.ErrReplyToNotFound, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsCwd, connect.CodeInvalidArgument},
		{agents.ErrWorktreeCwdNotPrimaryCheckout, connect.CodeInvalidArgument},
		{agents.ErrWorktreeUnsafePath, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsBase, connect.CodeInvalidArgument},
		{agents.ErrTooManyAgents, connect.CodeResourceExhausted},
		{mail.ErrMailboxFull, connect.CodeResourceExhausted},
		{mail.ErrExchangeCapped, connect.CodeResourceExhausted},
		{agents.ErrTooManyChildren, connect.CodeResourceExhausted},
		{reviews.ErrReviewNotYours, connect.CodePermissionDenied},
		{reviews.ErrReviewHumanOnly, connect.CodePermissionDenied},
		{reviews.ErrReviewNotEntitled, connect.CodePermissionDenied},
		{reviews.ErrReviewOwnWork, connect.CodePermissionDenied},
		{mail.ErrOutsideFamily, connect.CodePermissionDenied},
		{agents.ErrOutsideWorkflow, connect.CodePermissionDenied},
		{agentdirs.ErrOutsideAgents, connect.CodePermissionDenied},
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
