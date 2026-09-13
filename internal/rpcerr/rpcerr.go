// Package rpcerr turns the domain's sentinels into the connect codes callers see.
package rpcerr

import (
	"fmt"

	"weave/internal/fail"

	"connectrpc.com/connect"
)

// Wrap turns an error the domain returned into the one its caller sees.
func Wrap(err error, failure string) error {
	if err == nil {
		return nil
	}

	if sentinel := fail.Of(err); sentinel != nil {
		return connect.NewError(codeOf(sentinel.Kind()), err)
	}

	return connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", failure, err))
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
	default:
		return connect.CodeInternal
	}
}
