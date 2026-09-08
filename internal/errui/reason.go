package errui

import (
	"errors"

	"weave/internal/caller"

	"connectrpc.com/connect"
)

// GetTrace returns the id the service logged a failure under, blank for a failure that never
// reached the service or came back without one.
func GetTrace(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Meta().Get(caller.TraceHeader)
	}

	return ""
}

// GetReason reduces a service error to the part worth reading, a connect error stringifying with
// its code in front of a message that says more.
func GetReason(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}

	return err.Error()
}
