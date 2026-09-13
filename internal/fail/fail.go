// Package fail declares the sentinels the domain returns, each carrying the kind of failure it
// is so the transport can answer without a registry of its own.
//
// A sentinel built here is compared with errors.Is exactly as one from errors.New is, and the
// kind rides along for whoever has to turn it into a status code. Saying fills in what only the
// raise site knows, such as the id that was not found, in place of the sentinel's own wording.
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

// Sentinel is an error the domain declares once and callers compare against with errors.Is.
type Sentinel struct {
	kind    Kind
	message string
}

// Error returns the sentinel's own wording.
func (s *Sentinel) Error() string { return s.message }

// Kind returns what a caller can do about this failure.
func (s *Sentinel) Kind() Kind { return s.kind }

// Saying returns this sentinel worded for one request, replacing its own message.
func (s *Sentinel) Saying(format string, args ...any) error {
	return &said{sentinel: s, message: fmt.Sprintf(format, args...)}
}

// said is one sentinel worded for the request that raised it.
type said struct {
	sentinel *Sentinel
	message  string
}

func (s *said) Error() string { return s.message }

func (s *said) Unwrap() error { return s.sentinel }

// NotFound declares a sentinel for a thing the request named that does not exist.
func NotFound(message string) *Sentinel { return &Sentinel{kind: KindNotFound, message: message} }

// AlreadyExists declares a sentinel for a thing the request would create that exists already.
func AlreadyExists(message string) *Sentinel {
	return &Sentinel{kind: KindAlreadyExists, message: message}
}

// Precondition declares a sentinel for a state the request needs the system not to be in.
func Precondition(message string) *Sentinel {
	return &Sentinel{kind: KindPrecondition, message: message}
}

// InvalidArgument declares a sentinel for a request that is unusable as it stands.
func InvalidArgument(message string) *Sentinel {
	return &Sentinel{kind: KindInvalidArgument, message: message}
}

// Exhausted declares a sentinel for a limit the request would take past its ceiling.
func Exhausted(message string) *Sentinel { return &Sentinel{kind: KindExhausted, message: message} }

// Denied declares a sentinel for a request its caller is not allowed to make.
func Denied(message string) *Sentinel { return &Sentinel{kind: KindDenied, message: message} }

// Of returns the first sentinel in an error's tree, or nil when it holds none.
func Of(err error) *Sentinel {
	var sentinel *Sentinel
	if errors.As(err, &sentinel) {
		return sentinel
	}

	return nil
}
