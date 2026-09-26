// Package terminal is the ConnectRPC transport over the shells a person opens on the engine.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"komrad/internal/fail"
	"komrad/internal/pty"
	"komrad/internal/rpcerr"
	"komrad/internal/shells"
	terminalv1 "komrad/proto/gen/terminal/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// DrainAfterExit is how long output is still read after the shell ends.
const DrainAfterExit = 250 * time.Millisecond

// endOfInput is the byte a terminal reads as end of input, control-D.
const endOfInput = 0x04

var errNotOpened = fail.InvalidArgument("a shell stream opens with an open message")

type shellOpener interface {
	Open(ctx context.Context, params shells.OpenParams) (pty.Terminal, error)
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Shells         shellOpener
	DrainAfterExit time.Duration
}

// Handler serves shells over a stream.
type Handler struct {
	shells         shellOpener
	drainAfterExit time.Duration
}

// NewHandler builds the handler.
func NewHandler(params NewHandlerParams) *Handler {
	drain := params.DrainAfterExit
	if drain == 0 {
		drain = DrainAfterExit
	}

	return &Handler{shells: params.Shells, drainAfterExit: drain}
}

// Shell opens a shell from the stream's first message, carries input and resizes to it and its
// output back, and ends with its exit status.
func (h *Handler) Shell(ctx context.Context, stream *connect.BidiStream[terminalv1.ShellRequest, terminalv1.ShellResponse]) *fail.Error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	first, err := stream.Receive()
	if err != nil {
		return rpcerr.From(err, "failed to read the opening message")
	}

	open := first.GetOpen()
	if open == nil {
		return errNotOpened
	}

	terminal, err := h.shells.Open(ctx, shells.OpenParams{
		Dir:  open.GetDir(),
		Size: toSize(open.GetSize()),
		Term: open.GetTerm(),
	})
	if err != nil {
		return rpcerr.From(err, "failed to open the shell")
	}
	defer terminal.Close()

	ready := &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Ready{Ready: &emptypb.Empty{}}}
	if err := stream.Send(ready); err != nil {
		cancel()
		_, waitErr := terminal.Wait()

		return rpcerr.From(errors.Join(err, waitErr), "failed to send ready")
	}

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		sendOutput(stream, terminal)
	}()
	go func() {
		if err := receiveInput(stream, terminal); err != nil {
			cancel()
			_ = terminal.Close()
		}
	}()

	code, err := terminal.Wait()

	select {
	case <-drained:
	case <-time.After(h.drainAfterExit):
		_ = terminal.Close()
		<-drained
	}

	if err != nil {
		return rpcerr.From(err, "failed to wait for the shell")
	}

	exit := &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Exit{Exit: &terminalv1.Exit{Code: int32(min(code, math.MaxInt32))}}} //nolint:gosec // bounded above
	if err := stream.Send(exit); err != nil {
		return rpcerr.From(err, "failed to send the exit status")
	}

	return nil
}

func sendOutput(stream *connect.BidiStream[terminalv1.ShellRequest, terminalv1.ShellResponse], terminal pty.Terminal) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := terminal.Read(buffer)
		if n > 0 {
			output := &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Output{Output: append([]byte(nil), buffer[:n]...)}}
			if sendErr := stream.Send(output); sendErr != nil {
				_ = terminal.Close()

				return
			}
		}

		if err != nil {
			return
		}
	}
}

// receiveInput carries the caller's input to the terminal, typing end of input when the caller
// closes its side.
func receiveInput(stream *connect.BidiStream[terminalv1.ShellRequest, terminalv1.ShellResponse], terminal pty.Terminal) error {
	for {
		request, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			if _, err := terminal.Write([]byte{endOfInput}); err != nil {
				return fmt.Errorf("failed to end the shell's input: %w", err)
			}

			return nil
		}

		if err != nil {
			return fmt.Errorf("failed to hear from the caller: %w", err)
		}

		switch message := request.GetMessage().(type) {
		case *terminalv1.ShellRequest_Input:
			if _, err := terminal.Write(message.Input); err != nil {
				return fmt.Errorf("failed to type into the shell: %w", err)
			}
		case *terminalv1.ShellRequest_Resize:
			if err := terminal.Resize(toSize(message.Resize)); err != nil {
				return fmt.Errorf("failed to resize the shell: %w", err)
			}
		case *terminalv1.ShellRequest_Open:
		}
	}
}

func toSize(size *terminalv1.Size) pty.Size {
	return pty.Size{
		Rows: uint16(min(size.GetRows(), math.MaxUint16)), //nolint:gosec // bounded above
		Cols: uint16(min(size.GetCols(), math.MaxUint16)), //nolint:gosec // bounded above
	}
}
