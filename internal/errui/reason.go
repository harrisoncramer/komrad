package errui

import (
	"errors"

	"komrad/internal/caller"

	"connectrpc.com/connect"
)

// GetTrace returns the id the service logged a failure under, blank for a failure that never
// reached the service or came back without one.
func GetTrace(err error) string {
	if answered, ok := asAnswered(err); ok {
		return answered.Meta().Get(caller.TraceHeader)
	}

	return ""
}

// GetReason reduces a service error to the part worth reading.
func GetReason(err error) string {
	if answered, ok := asAnswered(err); ok {
		return answered.Message()
	}

	return err.Error()
}

// IsNotFound reports whether the service answered that what was asked for is not there.
func IsNotFound(err error) bool {
	answered, ok := asAnswered(err)

	return ok && answered.Code() == connect.CodeNotFound
}

// asAnswered is the refusal the service answered with, and false for an error that never
// reached it.
func asAnswered(err error) (*connect.Error, bool) {
	var answered *connect.Error
	if !errors.As(err, &answered) {
		return nil, false
	}

	return answered, true
}
