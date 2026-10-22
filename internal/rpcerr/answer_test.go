package rpcerr_test

import (
	"errors"
	"fmt"
	"testing"

	"komrad/internal/agentdirs"
	"komrad/internal/agenterr"
	"komrad/internal/agents"
	"komrad/internal/attaches"
	"komrad/internal/checkouts"
	"komrad/internal/documents"
	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logread"
	"komrad/internal/mail"
	"komrad/internal/reviews"
	"komrad/internal/rpcerr"
	"komrad/internal/schedules"
	"komrad/internal/scripts"
	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tasks"
	"komrad/internal/watch"
	"komrad/lib/pagination"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// answered is what a caller receives when a handler returns this failure.
func answered(err error, failure string) error {
	_, served := rpcerr.Answer[emptypb.Empty](nil, rpcerr.From(err, failure))

	return served
}

// TestWrapCodes pins the code that answers every sentinel a handler can receive.
func TestWrapCodes(t *testing.T) {
	tests := []struct {
		sentinel error
		want     connect.Code
	}{
		{store.ErrNotFound, connect.CodeNotFound},
		{agenterr.ErrAgentNotFound, connect.CodeNotFound},
		{agenterr.ErrWorkflowNotFound, connect.CodeNotFound},
		{schedules.ErrScheduleNotFound, connect.CodeNotFound},
		{agents.ErrScriptNotFound, connect.CodeNotFound},
		{mail.ErrMailNotFound, connect.CodeNotFound},
		{mail.ErrRecipientNotFound, connect.CodeNotFound},
		{reviews.ErrReviewThreadNotFound, connect.CodeNotFound},
		{reviews.ErrReviewMessageNotFound, connect.CodeNotFound},
		{reviews.ErrReviewNotFound, connect.CodeNotFound},
		{mail.ErrAgentNotFound, connect.CodeNotFound},
		{schedules.ErrWorkflowNotFound, connect.CodeNotFound},
		{schedules.ErrOrchestratorNotFound, connect.CodeNotFound},
		{logread.ErrNoSuchTrace, connect.CodeNotFound},
		{logread.ErrNoSuchRun, connect.CodeNotFound},
		{store.ErrNameTaken, connect.CodeAlreadyExists},
		{agenterr.ErrAlreadyLaunched, connect.CodeAlreadyExists},
		{forgestore.ErrHostTaken, connect.CodeAlreadyExists},
		{reviews.ErrForgeMessageAccepted, connect.CodeAlreadyExists},
		{agenterr.ErrAgentArchived, connect.CodeFailedPrecondition},
		{agents.ErrProvisionFailed, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotExecutable, connect.CodeFailedPrecondition},
		{agents.ErrScriptNotStartable, connect.CodeFailedPrecondition},
		{agents.ErrScriptDirMissing, connect.CodeFailedPrecondition},
		{agents.ErrScriptAlreadyRunning, connect.CodeFailedPrecondition},
		{agents.ErrWorkflowHasSchedules, connect.CodeFailedPrecondition},
		{agents.ErrAdoptNeedsWorktree, connect.CodeFailedPrecondition},
		{agents.ErrAdoptBranchInUse, connect.CodeFailedPrecondition},
		{agenterr.ErrWriterGoesWithTask, connect.CodeFailedPrecondition},
		{agenterr.ErrTaskArchived, connect.CodeFailedPrecondition},
		{agenterr.ErrReviewerNeedsDirectory, connect.CodeFailedPrecondition},
		{reviews.ErrReviewNoSnapshot, connect.CodeFailedPrecondition},
		{checkouts.ErrRebaseShallow, connect.CodeFailedPrecondition},
		{checkouts.ErrRebaseHiddenPaths, connect.CodeFailedPrecondition},
		{agents.ErrHookFailed, connect.CodeFailedPrecondition},
		{git.ErrBaseMissing, connect.CodeFailedPrecondition},
		{git.ErrBaseUnrelated, connect.CodeFailedPrecondition},
		{reviews.ErrReviewEmpty, connect.CodeFailedPrecondition},
		{reviews.ErrReviewAlreadyGiven, connect.CodeFailedPrecondition},
		{reviews.ErrReviewNotCurrent, connect.CodeFailedPrecondition},
		{agents.ErrAgentNotFailed, connect.CodeFailedPrecondition},
		{mail.ErrAgentArchived, connect.CodeFailedPrecondition},
		{checkouts.ErrNoWorktree, connect.CodeFailedPrecondition},
		{checkouts.ErrRebaseDirty, connect.CodeFailedPrecondition},
		{checkouts.ErrAbortNothing, connect.CodeFailedPrecondition},
		{git.ErrPushRejected, connect.CodeFailedPrecondition},
		{attaches.ErrNotLive, connect.CodeFailedPrecondition},
		{attaches.ErrNoPane, connect.CodeFailedPrecondition},
		{documentstore.ErrChanged, connect.CodeFailedPrecondition},
		{documenttypestore.ErrChanged, connect.CodeFailedPrecondition},
		{documenttypestore.ErrInUse, connect.CodeFailedPrecondition},
		{forge.ErrNotMergeable, connect.CodeFailedPrecondition},
		{forge.ErrNotDraftable, connect.CodeFailedPrecondition},
		{forge.ErrNoRereview, connect.CodeFailedPrecondition},
		{forge.ErrNoAutoMerge, connect.CodeFailedPrecondition},
		{schedules.ErrWorkflowArchived, connect.CodeFailedPrecondition},
		{schedules.ErrOrchestratorArchived, connect.CodeFailedPrecondition},
		{tasks.ErrTaskNotLive, connect.CodeFailedPrecondition},
		{taskstore.ErrNotesChanged, connect.CodeFailedPrecondition},
		{watch.ErrClosed, connect.CodeFailedPrecondition},
		{schedules.ErrBadCron, connect.CodeInvalidArgument},
		{pagination.ErrBadCursor, connect.CodeInvalidArgument},
		{agents.ErrUnknownHookEvent, connect.CodeInvalidArgument},
		{agents.ErrUnknownHookScript, connect.CodeInvalidArgument},
		{eventprompts.ErrUnknownEventPrompt, connect.CodeInvalidArgument},
		{eventprompts.ErrDuplicateEventPrompt, connect.CodeInvalidArgument},
		{eventprompts.ErrPromptTooLong, connect.CodeInvalidArgument},
		{mail.ErrKindMissing, connect.CodeInvalidArgument},
		{agents.ErrBadDirectory, connect.CodeInvalidArgument},
		{agentdirs.ErrNoDirectory, connect.CodeInvalidArgument},
		{agents.ErrAdoptNotABranch, connect.CodeInvalidArgument},
		{agents.ErrHarnessUnknown, connect.CodeInvalidArgument},
		{agenterr.ErrSpawnerUnknown, connect.CodeInvalidArgument},
		{agenterr.ErrReviewerOwnsLaunch, connect.CodeInvalidArgument},
		{mail.ErrSelfAddressed, connect.CodeInvalidArgument},
		{mail.ErrRecipientAmbiguous, connect.CodeInvalidArgument},
		{mail.ErrSenderUnknown, connect.CodeInvalidArgument},
		{mail.ErrReplyToNotFound, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsCwd, connect.CodeInvalidArgument},
		{agents.ErrWorktreeCwdNotPrimaryCheckout, connect.CodeInvalidArgument},
		{workflowstore.ErrWorktreeUnsafePath, connect.CodeInvalidArgument},
		{agents.ErrWorktreeNeedsBase, connect.CodeInvalidArgument},
		{schedules.ErrNotAnOrchestrator, connect.CodeInvalidArgument},
		{schedules.ErrPromptTooLong, connect.CodeInvalidArgument},
		{taskstore.ErrBadSlug, connect.CodeInvalidArgument},
		{documents.ErrAgentWritingForAnother, connect.CodeInvalidArgument},
		{documents.ErrNoAuthor, connect.CodeInvalidArgument},
		{documents.ErrNoTypeVersion, connect.CodeInvalidArgument},
		{documents.ErrNoVersion, connect.CodeInvalidArgument},
		{documents.ErrTooLarge, connect.CodeInvalidArgument},
		{documents.ErrUnreadablePath, connect.CodeInvalidArgument},
		{documentstore.ErrUnknownType, connect.CodeInvalidArgument},
		{forge.ErrNotPullRequest, connect.CodeInvalidArgument},
		{git.ErrNotForgeRemote, connect.CodeInvalidArgument},
		{pullrequeststore.ErrInvalidPattern, connect.CodeInvalidArgument},
		{agenterr.ErrTooManyAgents, connect.CodeResourceExhausted},
		{mail.ErrMailboxFull, connect.CodeResourceExhausted},
		{mail.ErrExchangeCapped, connect.CodeResourceExhausted},
		{agenterr.ErrTooManyReviewers, connect.CodeResourceExhausted},
		{agenterr.ErrTooManyTasks, connect.CodeResourceExhausted},
		{reviews.ErrReviewNotYours, connect.CodePermissionDenied},
		{reviews.ErrReviewHumanOnly, connect.CodePermissionDenied},
		{reviews.ErrReviewNotEntitled, connect.CodePermissionDenied},
		{reviews.ErrReviewOwnWork, connect.CodePermissionDenied},
		{mail.ErrOutsideTask, connect.CodePermissionDenied},
		{agenterr.ErrOutsideWorkflow, connect.CodePermissionDenied},
		{agentdirs.ErrOutsideAgents, connect.CodePermissionDenied},
		{tasks.ErrNotesNotYours, connect.CodePermissionDenied},
		{documents.ErrDeleteByAgent, connect.CodePermissionDenied},
		{documents.ErrDocumentTypeByAgent, connect.CodePermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.sentinel.Error(), func(t *testing.T) {
			bare := answered(tt.sentinel, "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(bare))

			wrapped := answered(fmt.Errorf("%w: %q", tt.sentinel, "abc"), "failed to do the thing")
			assert.Equal(t, tt.want, connect.CodeOf(wrapped), "a sentinel is still found under a wrapping")
			require.ErrorContains(t, wrapped, `"abc"`, "and what the domain added to it is kept")
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

// TestWrapCarriesTwoSentinels covers an error holding two sentinels at once, where the order of wrapping decides the code.
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
	require.ErrorContains(t, err, "failed to archive agents")
	assert.ErrorContains(t, err, "connection refused")
}

// TestWrapSaying covers a sentinel worded for the request that raised it, which keeps its kind
// and replaces its own message.
func TestWrapSaying(t *testing.T) {
	err := answered(store.ErrNotFound.Saying("no schedule with id %q", "s-1"), "failed to get schedule")

	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	require.ErrorContains(t, err, `no schedule with id "s-1"`)
	assert.NotContains(t, err.Error(), "not found:", "the sentinel's own message is replaced, not appended")
}

// TestWrapSayingUnderAWrapping covers a worded sentinel the domain put its own prefix on, where
// the caller is told the sentence the raise site chose and nothing else.
func TestWrapSayingUnderAWrapping(t *testing.T) {
	said := store.ErrNotFound.Saying("no schedule with id %q", "s-1")

	err := answered(fmt.Errorf("failed to get schedule %q: %w", "s-1", said), "failed to get schedule")

	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	assert.EqualError(t, err, `not_found: no schedule with id "s-1"`)
}

// TestWrapKeepsTheMappedCodeBesideAnUnmappedSentinel covers an error carrying a plain error
// beside a sentinel, which must not cost it the sentinel's code.
func TestWrapKeepsTheMappedCodeBesideAnUnmappedSentinel(t *testing.T) {
	ran := fmt.Errorf("%w: %w: exited 1", agents.ErrProvisionFailed, scripts.ErrFailed)

	err := answered(ran, "failed to create the agent")

	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

// TestNew pins the connect code New carries for each kind.
func TestNew(t *testing.T) {
	type newCase struct {
		name string
		kind fail.Kind
		want connect.Code
	}

	tests := []newCase{
		{
			name: "an unknown kind is internal",
			kind: fail.Kind(99),
			want: connect.CodeInternal,
		},
		{
			name: "not found",
			kind: fail.KindNotFound,
			want: connect.CodeNotFound,
		},
		{
			name: "already exists",
			kind: fail.KindAlreadyExists,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "precondition",
			kind: fail.KindPrecondition,
			want: connect.CodeFailedPrecondition,
		},
		{
			name: "invalid argument",
			kind: fail.KindInvalidArgument,
			want: connect.CodeInvalidArgument,
		},
		{
			name: "exhausted",
			kind: fail.KindExhausted,
			want: connect.CodeResourceExhausted,
		},
		{
			name: "denied",
			kind: fail.KindDenied,
			want: connect.CodePermissionDenied,
		},
		{
			name: "internal",
			kind: fail.KindInternal,
			want: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, connect.CodeOf(rpcerr.New(tt.kind, errors.New("refused"))))
		})
	}
}

// TestFromConnectCode pins the kind a connect error already carrying a code keeps.
func TestFromConnectCode(t *testing.T) {
	type fromConnectCodeCase struct {
		code connect.Code
		want connect.Code
	}

	tests := []fromConnectCodeCase{
		{
			code: connect.CodeUnavailable,
			want: connect.CodeInternal,
		},
		{
			code: connect.CodeCanceled,
			want: connect.CodeInternal,
		},
		{
			code: connect.CodeNotFound,
			want: connect.CodeNotFound,
		},
		{
			code: connect.CodePermissionDenied,
			want: connect.CodePermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			err := answered(connect.NewError(tt.code, errors.New("refused")), "failed to do the thing")

			assert.Equal(t, tt.want, connect.CodeOf(err))
		})
	}
}
