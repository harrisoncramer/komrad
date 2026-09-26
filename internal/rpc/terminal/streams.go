package terminal

import (
	"fmt"

	"komrad/internal/pty"
	terminalv1 "komrad/proto/gen/terminal/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// request is what a caller sent once the terminal is open: bytes typed, a new size, or neither.
type request struct {
	input  []byte
	resize *terminalv1.Size
}

// terminalStream is one kind of terminal stream, spoken in its own messages.
type terminalStream interface {
	receive() (request, error)
	sendReady() error
	sendOutput(output []byte) error
	sendExit(code int32) error
	endInput(terminal pty.Terminal) error
}

type shellStream struct {
	stream *connect.BidiStream[terminalv1.ShellRequest, terminalv1.ShellResponse]
}

func (s shellStream) receive() (request, error) {
	message, err := s.stream.Receive()
	if err != nil {
		return request{}, err //nolint:wrapcheck // the caller tells io.EOF apart
	}

	return request{input: message.GetInput(), resize: message.GetResize()}, nil
}

func (s shellStream) sendReady() error {
	return s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Ready{Ready: &emptypb.Empty{}}})
}

func (s shellStream) sendOutput(output []byte) error {
	return s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Output{Output: output}})
}

func (s shellStream) sendExit(code int32) error {
	return s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Exit{Exit: &terminalv1.Exit{Code: code}}})
}

// endInput types end of input, so a shell given piped input runs it and exits.
func (shellStream) endInput(terminal pty.Terminal) error {
	if _, err := terminal.Write([]byte{endOfInput}); err != nil {
		return fmt.Errorf("failed to end the shell's input: %w", err)
	}

	return nil
}

type attachStream struct {
	stream *connect.BidiStream[terminalv1.AttachRequest, terminalv1.AttachResponse]
}

func (s attachStream) receive() (request, error) {
	message, err := s.stream.Receive()
	if err != nil {
		return request{}, err //nolint:wrapcheck // the caller tells io.EOF apart
	}

	return request{input: message.GetInput(), resize: message.GetResize()}, nil
}

func (s attachStream) sendReady() error {
	return s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Ready{Ready: &emptypb.Empty{}}})
}

func (s attachStream) sendOutput(output []byte) error {
	return s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Output{Output: output}})
}

func (s attachStream) sendExit(code int32) error {
	return s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Exit{Exit: &terminalv1.Exit{Code: code}}})
}

// endInput types nothing, since the agent's own terminal is not the caller's to end.
func (attachStream) endInput(pty.Terminal) error {
	return nil
}
