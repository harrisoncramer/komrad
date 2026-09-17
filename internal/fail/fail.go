// Package fail declares the sentinels the domain returns, each carrying the kind of failure it
// is so the transport can answer without a registry of its own.
//
// A sentinel built here is compared with errors.Is exactly as one from errors.New is, and the
// kind rides along for whoever has to turn it into a status code. Saying fills in what only the
// raise site knows, such as the id that was not found, in place of the sentinel's own wording.
//
// Answer hands back that wording as a string rather than the error itself because
// the domain may have wrapped the sentinel on its way up. Those prefixes say what the code was
// doing, which belongs in the log, while the caller wants the one sentence the raise site chose
// about their own request.
package fail

import (
	"errors"
	"fmt"
)

// Kind is what a caller can do about a failure.
type Kind int

const (
	// KindInternal is the failure a caller cannot act on.
	KindInternal Kind = iota
	// KindNotFound is the thing named by the request not existing.
	KindNotFound
	// KindAlreadyExists is the thing the request would create existing already.
	KindAlreadyExists
	// KindPrecondition is the system being in a state the request needs it not to be in.
	KindPrecondition
	// KindInvalidArgument is the request itself being unusable.
	KindInvalidArgument
	// KindExhausted is a limit the request would take past its ceiling.
	KindExhausted
	// KindDenied is the caller not being allowed to ask.
	KindDenied
)

// Error is a sentinel the domain declares once and callers compare against with errors.Is.
type Error struct {
	kind    Kind
	message string
	wrapped error
	worded  bool
}

// Error returns the sentinel's own wording.
func (s *Error) Error() string { return s.message }

// Kind returns what a caller can do about this failure.
func (s *Error) Kind() Kind { return s.kind }

// Saying returns this sentinel worded for one request, replacing its own message.
func (s *Error) Saying(format string, args ...any) *Error {
	return &Error{
		kind:    s.kind,
		message: fmt.Sprintf(format, args...),
		wrapped: s,
		worded:  true,
	}
}

// Worded says whether a raise site chose this failure's message for one request, which is what
// a caller is told in place of whatever the domain wrapped it in.
func (s *Error) Worded() bool { return s.worded }

// Unwrap returns the sentinel this one was worded from, or the failure it was built around.
func (s *Error) Unwrap() error { return s.wrapped }

// Kinded returns this failure as a different kind, for a code somebody else already decided.
func (s *Error) Kinded(kind Kind) *Error {
	return &Error{
		kind:    kind,
		message: s.message,
		wrapped: s.wrapped,
		worded:  s.worded,
	}
}

// Because returns an internal failure carrying the error that caused it.
func Because(err error, message string) *Error {
	return &Error{
		kind:    KindInternal,
		message: message,
		wrapped: err,
		worded:  true,
	}
}

// Internal declares a sentinel for a failure the caller did nothing to cause and cannot fix.
func Internal(message string) *Error { return &Error{kind: KindInternal, message: message} }

// NotFound declares a sentinel for a thing the request named that does not exist.
func NotFound(message string) *Error { return &Error{kind: KindNotFound, message: message} }

// AlreadyExists declares a sentinel for a thing the request would create that exists already.
func AlreadyExists(message string) *Error {
	return &Error{kind: KindAlreadyExists, message: message}
}

// Precondition declares a sentinel for a state the request needs the system not to be in.
func Precondition(message string) *Error {
	return &Error{kind: KindPrecondition, message: message}
}

// InvalidArgument declares a sentinel for a request that is unusable as it stands.
func InvalidArgument(message string) *Error {
	return &Error{kind: KindInvalidArgument, message: message}
}

// Exhausted declares a sentinel for a limit the request would take past its ceiling.
func Exhausted(message string) *Error { return &Error{kind: KindExhausted, message: message} }

// Denied declares a sentinel for a request its caller is not allowed to make.
func Denied(message string) *Error { return &Error{kind: KindDenied, message: message} }

// Of returns the failure an error answers with, or nil when it holds none.
func Of(err error) *Error {
	var found *Error
	if errors.As(err, &found) {
		return found
	}

	return nil
}
