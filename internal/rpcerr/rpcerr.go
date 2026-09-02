// Package rpcerr maps the domain's sentinels to the connect codes callers see, so a handler
// names a code only for a failure it raises itself.
package rpcerr

import (
	"errors"
	"fmt"

	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/caller"
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
				return marked(err, connect.NewError(entry.code, errors.New(replacement.message)))
			}
		}

		return marked(err, connect.NewError(entry.code, err))
	}

	return marked(err, connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", failure, err)))
}

// marked says an action ran and exited non-zero, which is the one failure whose output the
// caller may already be watching in the pane it ran in.
func marked(cause error, wrapped *connect.Error) error {
	if errors.Is(cause, actions.ErrFailed) {
		wrapped.Meta().Set(caller.SetupRanHeader, "true")
	}

	return wrapped
}
