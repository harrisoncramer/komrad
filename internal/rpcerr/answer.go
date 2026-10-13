// Package rpcerr is where a handler's failure meets the connect code the caller gets,
// so one place satisfies the generated signature rather than every handler.
package rpcerr

import (
	"errors"
	"fmt"

	"komrad/internal/fail"

	"connectrpc.com/connect"
)

// codes pairs each kind of failure with the connect code that answers it.
var codes = []struct {
	kind fail.Kind
	code connect.Code
}{
	{kind: fail.KindNotFound, code: connect.CodeNotFound},
	{kind: fail.KindAlreadyExists, code: connect.CodeAlreadyExists},
	{kind: fail.KindPrecondition, code: connect.CodeFailedPrecondition},
	{kind: fail.KindInvalidArgument, code: connect.CodeInvalidArgument},
	{kind: fail.KindExhausted, code: connect.CodeResourceExhausted},
	{kind: fail.KindDenied, code: connect.CodePermissionDenied},
	{kind: fail.KindInternal, code: connect.CodeInternal},
}

// Answer turns what a handler returned into what connect serves.
func Answer[T any](message *T, err *fail.Error) (*connect.Response[T], error) {
	if err != nil {
		return nil, connect.NewError(codeOf(err.Kind()), err)
	}

	return connect.NewResponse(message), nil
}

// AnswerStream turns what a streaming handler returned into what connect serves once the
// stream ends.
func AnswerStream(err *fail.Error) error {
	if err != nil {
		return connect.NewError(codeOf(err.Kind()), err)
	}

	return nil
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
		if !found.IsWorded() && err.Error() != found.Error() {
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

// New is a connect error carrying the code for kind.
func New(kind fail.Kind, err error) error {
	return connect.NewError(codeOf(kind), err)
}

// Unavailable is a connect error for a failure the caller may retry.
func Unavailable(err error) error {
	return connect.NewError(connect.CodeUnavailable, err)
}

// codeOf says which connect code answers a kind of failure.
func codeOf(kind fail.Kind) connect.Code {
	for _, pair := range codes {
		if pair.kind == kind {
			return pair.code
		}
	}

	return connect.CodeInternal
}

// kindOf says which kind of failure a code already decided on stands for.
func kindOf(code connect.Code) fail.Kind {
	for _, pair := range codes {
		if pair.code == code {
			return pair.kind
		}
	}

	return fail.KindInternal
}
