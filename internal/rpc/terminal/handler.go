// Package terminal is the ConnectRPC transport over the terminals a person opens on the engine.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"komrad/internal/attaches"
	"komrad/internal/fail"
	"komrad/internal/pty"
	"komrad/internal/rpcerr"
	"komrad/internal/shells"
	terminalv1 "komrad/proto/gen/terminal/v1"

	"connectrpc.com/connect"
)

const (
	// defaultDrainAfterExit is how long output is still read after the program ends.
	defaultDrainAfterExit = 250 * time.Millisecond
	// endOfInput is the byte a terminal reads as end of input, control-D.
	endOfInput = 0x04
)

var (
	errShellNotOpened  = fail.InvalidArgument("a shell stream opens with an open message")
	errAttachNotOpened = fail.InvalidArgument("an attach stream opens with an open message")
)

type shellOpener interface {
	Open(ctx context.Context, params shells.OpenParams) (pty.Terminal, error)
}

type attachOpener interface {
	Open(ctx context.Context, params attaches.OpenParams) (pty.Terminal, error)
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Shells         shellOpener
	Attaches       attachOpener
	DrainAfterExit time.Duration
}

// Handler serves terminals over a stream.
type Handler struct {
	shells         shellOpener
	attaches       attachOpener
	drainAfterExit time.Duration
}

// NewHandler builds the handler.
func NewHandler(params NewHandlerParams) *Handler {
	drain := params.DrainAfterExit
	if drain == 0 {
		drain = defaultDrainAfterExit
	}

	return &Handler{shells: params.Shells, attaches: params.Attaches, drainAfterExit: drain}
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
		return errShellNotOpened
	}

	terminal, err := h.shells.Open(ctx, shells.OpenParams{
		Dir:  open.GetDir(),
		Size: toSize(open.GetSize()),
		Term: open.GetTerm(),
	})
	if err != nil {
		return rpcerr.From(err, "failed to open the shell")
	}

	return h.carry(cancel, shellStream{stream: stream}, terminal)
}

// Attach attaches to an agent's terminal from the stream's first message, carries input and
// resizes to it and its output back, and ends with the attach's exit status.
func (h *Handler) Attach(ctx context.Context, stream *connect.BidiStream[terminalv1.AttachRequest, terminalv1.AttachResponse]) *fail.Error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	first, err := stream.Receive()
	if err != nil {
		return rpcerr.From(err, "failed to read the opening message")
	}

	open := first.GetOpen()
	if open == nil {
		return errAttachNotOpened
	}

	terminal, err := h.attaches.Open(ctx, attaches.OpenParams{
		AgentID: open.GetAgentId(),
		Size:    toSize(open.GetSize()),
		Term:    open.GetTerm(),
	})
	if err != nil {
		return rpcerr.From(err, "failed to attach to the agent")
	}

	return h.carry(cancel, attachStream{stream: stream}, terminal)
}

// carry says the terminal is ready, carries the caller's input to it and its output back, and
// ends with its exit status.
func (h *Handler) carry(cancel context.CancelFunc, stream terminalStream, terminal pty.Terminal) *fail.Error {
	defer terminal.Close()

	if err := stream.sendReady(); err != nil {
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
		return rpcerr.From(err, "failed to wait for the terminal")
	}

	if err := stream.sendExit(int32(min(code, math.MaxInt32))); err != nil { //nolint:gosec // bounded above
		return rpcerr.From(err, "failed to send the exit status")
	}

	return nil
}

func sendOutput(stream terminalStream, terminal pty.Terminal) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := terminal.Read(buffer)
		if n > 0 {
			if sendErr := stream.sendOutput(append([]byte(nil), buffer[:n]...)); sendErr != nil {
				_ = terminal.Close()

				return
			}
		}

		if err != nil {
			return
		}
	}
}

// receiveInput carries the caller's input to the terminal, ending the terminal's input the way
// the stream says when the caller closes its side.
func receiveInput(stream terminalStream, terminal pty.Terminal) error {
	for {
		request, err := stream.receive()
		if errors.Is(err, io.EOF) {
			return stream.endInput(terminal)
		}

		if err != nil {
			return fmt.Errorf("failed to hear from the caller: %w", err)
		}

		if request.input != nil {
			if _, err := terminal.Write(request.input); err != nil {
				return fmt.Errorf("failed to type into the terminal: %w", err)
			}
		}

		if request.resize != nil {
			if err := terminal.Resize(toSize(request.resize)); err != nil {
				return fmt.Errorf("failed to resize the terminal: %w", err)
			}
		}
	}
}

func toSize(size *terminalv1.Size) pty.Size {
	return pty.Size{
		Rows: uint16(min(size.GetRows(), math.MaxUint16)), //nolint:gosec // bounded above
		Cols: uint16(min(size.GetCols(), math.MaxUint16)), //nolint:gosec // bounded above
	}
}
