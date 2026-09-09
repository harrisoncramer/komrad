// Package rpcerr maps the domain's sentinels to the connect codes callers see, so a handler
// names a code only for a failure it raises itself.
package rpcerr

import (
	"errors"
	"fmt"

	"weave/internal/agents"
	"weave/internal/browser"
	"weave/internal/git"
	"weave/internal/store"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
)

type mapping struct {
	sentinel error
	code     connect.Code
}

// No sentinel here wraps another, so the order is only for reading.
var table = []mapping{
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
	{agents.ErrReviewerAlreadyRunning, connect.CodeAlreadyExists},
	{agents.ErrAdoptAlreadyAdopted, connect.CodeAlreadyExists},

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
	{agents.ErrResumeInFlight, connect.CodeFailedPrecondition},
	{agents.ErrRebaseNoWorktree, connect.CodeFailedPrecondition},
	{agents.ErrRebaseAdopted, connect.CodeFailedPrecondition},
	{agents.ErrRebaseDirty, connect.CodeFailedPrecondition},
	{agents.ErrRebaseInProgress, connect.CodeFailedPrecondition},
	{agents.ErrRebaseDetached, connect.CodeFailedPrecondition},
	{agents.ErrRebaseBusy, connect.CodeFailedPrecondition},
	{git.ErrRebaseConflicted, connect.CodeFailedPrecondition},
	{agents.ErrChildNeedsParentDirectory, connect.CodeFailedPrecondition},
	{agents.ErrNoPullRequest, connect.CodeFailedPrecondition},
	{agents.ErrReviewNoRound, connect.CodeFailedPrecondition},
	{agents.ErrReviewEmpty, connect.CodeFailedPrecondition},
	{agents.ErrReviewInFlight, connect.CodeFailedPrecondition},
	{agents.ErrReviewNotAnswered, connect.CodeFailedPrecondition},
	{agents.ErrReviewNotCurrent, connect.CodeFailedPrecondition},
	{agents.ErrReviewAlreadyGiven, connect.CodeFailedPrecondition},
	{agents.ErrReviewNeedsOwnWork, connect.CodeFailedPrecondition},

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
	{agents.ErrWorktreeCwdNotPrimaryCheckout, connect.CodeInvalidArgument},
	{agents.ErrWorktreeUnsafePath, connect.CodeInvalidArgument},
	{browser.ErrNotAWebURL, connect.CodeInvalidArgument},

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
}

// Replacement is what a caller is told in place of a sentinel's own message, given at the
// call site because it names the request's arguments.
type Replacement struct {
	sentinel error
	message  string
}

// Saying builds the message shown in place of the sentinel's own, for one whose message says
// less than the request can.
func Saying(sentinel error, format string, args ...any) Replacement {
	return Replacement{sentinel: sentinel, message: fmt.Sprintf(format, args...)}
}

// Wrap turns an error the domain returned into the one its caller sees: a sentinel keeps its
// own message, or a replacement's where one is given for it, under the code mapped to it, and
// anything else is the panel failing at something the caller cannot fix, so it is internal
// under the failure described.
func Wrap(err error, failure string, replacements ...Replacement) error {
	if err == nil {
		return nil
	}

	for _, entry := range table {
		if !errors.Is(err, entry.sentinel) {
			continue
		}

		for _, replacement := range replacements {
			if errors.Is(err, replacement.sentinel) {
				return connect.NewError(entry.code, errors.New(replacement.message))
			}
		}

		return connect.NewError(entry.code, err)
	}

	return connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", failure, err))
}
