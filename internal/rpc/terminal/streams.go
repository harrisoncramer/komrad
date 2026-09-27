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

// callerReadWriter reads what a caller sends and writes back to it, in one service's messages.
type callerReadWriter interface {
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
		return request{}, fmt.Errorf("failed to read the caller's message: %w", err)
	}

	return request{input: message.GetInput(), resize: message.GetResize()}, nil
}

func (s shellStream) sendReady() error {
	if err := s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Ready{Ready: &emptypb.Empty{}}}); err != nil {
		return fmt.Errorf("failed to send ready: %w", err)
	}

	return nil
}

func (s shellStream) sendOutput(output []byte) error {
	if err := s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Output{Output: output}}); err != nil {
		return fmt.Errorf("failed to send output: %w", err)
	}

	return nil
}

func (s shellStream) sendExit(code int32) error {
	if err := s.stream.Send(&terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Exit{Exit: &terminalv1.Exit{Code: code}}}); err != nil {
		return fmt.Errorf("failed to send the exit status: %w", err)
	}

	return nil
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
		return request{}, fmt.Errorf("failed to read the caller's message: %w", err)
	}

	return request{input: message.GetInput(), resize: message.GetResize()}, nil
}

func (s attachStream) sendReady() error {
	if err := s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Ready{Ready: &emptypb.Empty{}}}); err != nil {
		return fmt.Errorf("failed to send ready: %w", err)
	}

	return nil
}

func (s attachStream) sendOutput(output []byte) error {
	if err := s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Output{Output: output}}); err != nil {
		return fmt.Errorf("failed to send output: %w", err)
	}

	return nil
}

func (s attachStream) sendExit(code int32) error {
	if err := s.stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Exit{Exit: &terminalv1.Exit{Code: code}}}); err != nil {
		return fmt.Errorf("failed to send the exit status: %w", err)
	}

	return nil
}

// endInput types nothing.
func (attachStream) endInput(pty.Terminal) error {
	return nil
}
