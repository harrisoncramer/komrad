package errui

import (
	"errors"

	"weave/internal/caller"

	"connectrpc.com/connect"
)

// GetTrace is the id the service logged a failure under, which it returns on the error's
// metadata, and blank for a failure that never reached the service or came back without one.
func GetTrace(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Meta().Get(caller.TraceHeader)
	}

	return ""
}

// GetReason reduces a service error to the part worth reading: a connect error stringifies
// with its code in front of the message, and the code says less to a person reading it
// than the message does.
func GetReason(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}

	return err.Error()
}
