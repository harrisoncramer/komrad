// Package rpcerr is where a handler's failure meets the connect code a caller is answered with,
// so the generated signature is satisfied in one place rather than in every handler.
package rpcerr

import (
	"errors"
	"fmt"

	"weave/internal/fail"

	"connectrpc.com/connect"
)

// Answer turns what a handler returned into what connect serves, which is where a handler
// written against the strict signature meets the one the generated interface fixes.
func Answer[T any](message *T, err *fail.Error) (*connect.Response[T], error) {
	if err != nil {
		return nil, connect.NewError(codeOf(err.Kind()), err)
	}

	return connect.NewResponse(message), nil
}

// From is how an error the domain returned becomes one a handler may answer with, a sentinel
// keeping its kind and anything else becoming internal.
func From(err error, failure string) *fail.Error {
	if err == nil {
		return nil
	}

	// A sentinel nobody worded says only what it was declared as, so whatever the domain added
	// around it on the way up is the detail about this request.
	if found := fail.Of(err); found != nil {
		if !found.Worded() && err.Error() != found.Error() {
			return found.Saying("%s", err.Error())
		}

		return found
	}

	var answered *connect.Error
	if errors.As(err, &answered) {
		return fail.Because(answered, answered.Message()).Kinded(kindOf(answered.Code()))
	}

	return fail.Because(err, fmt.Sprintf("%s: %s", failure, err))
}

// codeOf says which connect code answers a kind of failure.
func codeOf(kind fail.Kind) connect.Code {
	switch kind {
	case fail.KindNotFound:
		return connect.CodeNotFound
	case fail.KindAlreadyExists:
		return connect.CodeAlreadyExists
	case fail.KindPrecondition:
		return connect.CodeFailedPrecondition
	case fail.KindInvalidArgument:
		return connect.CodeInvalidArgument
	case fail.KindExhausted:
		return connect.CodeResourceExhausted
	case fail.KindDenied:
		return connect.CodePermissionDenied
	case fail.KindInternal:
		return connect.CodeInternal
	}

	return connect.CodeInternal
}

// kindOf says which kind of failure a code already decided on stands for.
func kindOf(code connect.Code) fail.Kind {
	switch code {
	case connect.CodeNotFound:
		return fail.KindNotFound
	case connect.CodeAlreadyExists:
		return fail.KindAlreadyExists
	case connect.CodeFailedPrecondition:
		return fail.KindPrecondition
	case connect.CodeInvalidArgument:
		return fail.KindInvalidArgument
	case connect.CodeResourceExhausted:
		return fail.KindExhausted
	case connect.CodePermissionDenied:
		return fail.KindDenied
	case connect.CodeCanceled, connect.CodeUnknown, connect.CodeDeadlineExceeded, connect.CodeAborted,
		connect.CodeOutOfRange, connect.CodeUnimplemented, connect.CodeInternal, connect.CodeUnavailable,
		connect.CodeDataLoss, connect.CodeUnauthenticated:
		return fail.KindInternal
	}

	return fail.KindInternal
}
