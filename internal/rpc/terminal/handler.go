package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"time"

	"komrad/internal/attaches"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/pty"
	"komrad/internal/rpcerr"
	"komrad/internal/shells"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1komrad"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
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

type sshServer interface {
	Serve(ctx context.Context, conn net.Conn)
	PublicKey() string
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Shells         shellOpener
	Attaches       attachOpener
	SSH            sshServer
	DrainAfterExit time.Duration
}

// Handler serves terminals over a stream.
type Handler struct {
	shells         shellOpener
	attaches       attachOpener
	ssh            sshServer
	drainAfterExit time.Duration
}

// NewHandler builds the handler.
func NewHandler(params NewHandlerParams) *Handler {
	drain := params.DrainAfterExit
	if drain == 0 {
		drain = defaultDrainAfterExit
	}

	return &Handler{
		shells:         params.Shells,
		attaches:       params.Attaches,
		ssh:            params.SSH,
		drainAfterExit: drain,
	}
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

	return h.carry(ctx, carryParams{
		Cancel:   cancel,
		Stream:   shellStream{stream: stream},
		Terminal: terminal,
	})
}

// Attach attaches to an agent's terminal from the stream's first message, carries input and
// resizes to it and its output back, and ends with the attach's exit status.
func (h *Handler) Attach(ctx context.Context, stream *terminalv1komrad.AttachStream) *fail.Error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	first, err := stream.Receive()
	if err != nil {
		return rpcerr.From(err, "failed to read the opening message")
	}

	open, ok := first.Message.(*terminalv1komrad.AttachRequest_Open)
	if !ok {
		return errAttachNotOpened
	}

	terminal, err := h.attaches.Open(ctx, attaches.OpenParams{
		AgentID: open.Open.AgentId,
		Size:    toSize(open.Open.Size),
		Term:    open.Open.Term,
	})
	if err != nil {
		return rpcerr.From(err, "failed to attach to the agent")
	}

	return h.carry(ctx, carryParams{
		Cancel:   cancel,
		Stream:   attachStream{stream: stream},
		Terminal: terminal,
	})
}

// SSH carries one SSH connection between the stream and the engine's SSH server until either side
// closes it.
func (h *Handler) SSH(ctx context.Context, stream *terminalv1komrad.SSHStream) *fail.Error {
	h.ssh.Serve(ctx, newSSHConn(ctx, stream))

	return nil
}

// GetSSHHostKey returns the public key the SSH server proves itself with.
func (h *Handler) GetSSHHostKey(context.Context, *emptypb.Empty) (*terminalv1.GetSSHHostKeyResponse, *fail.Error) {
	return &terminalv1.GetSSHHostKeyResponse{PublicKey: h.ssh.PublicKey()}, nil
}

type carryParams struct {
	Cancel   context.CancelFunc
	Stream   callerReadWriter
	Terminal pty.Terminal
}

// carry says the terminal is ready, carries the caller's input to it and its output back, and
// ends with its exit status.
func (h *Handler) carry(ctx context.Context, params carryParams) *fail.Error {
	cancel, stream, terminal := params.Cancel, params.Stream, params.Terminal
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
		if err := receiveInput(ctx, stream, terminal); err != nil {
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

func sendOutput(stream callerReadWriter, terminal pty.Terminal) {
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
func receiveInput(ctx context.Context, stream callerReadWriter, terminal pty.Terminal) error {
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

		if request.token != "" {
			if err := caller.Renew(ctx, request.token); err != nil {
				return fmt.Errorf("failed to renew the caller's sign-in: %w", err)
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
