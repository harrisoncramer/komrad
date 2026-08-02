package server

import (
	"fmt"

	"connectrpc.com/vanguard"
)

// newTranscoder puts the services behind one handler that answers each of them in whichever
// protocol the caller asked in. A Connect or gRPC client reaches the same handler it always
// did, and a request shaped by the google.api.http rules in the protos is translated into
// the same call, so REST is a second door onto the handlers rather than a second
// implementation of them.
//
// No codec is configured, so JSON is the protobuf JSON mapping and nothing about the wire
// format is weave's to maintain.
func newTranscoder(services []*vanguard.Service) (*vanguard.Transcoder, error) {
	transcoder, err := vanguard.NewTranscoder(services)
	if err != nil {
		return nil, fmt.Errorf("failed to set up the transcoder: %w", err)
	}

	return transcoder, nil
}
