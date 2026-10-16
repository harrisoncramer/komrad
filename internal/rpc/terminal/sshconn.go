package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"komrad/internal/caller"
	terminalv1 "komrad/proto/gen/terminal/v1"

	"connectrpc.com/connect"
)

type sshStream = connect.BidiStream[terminalv1.SSHRequest, terminalv1.SSHResponse]

// sshConn is an SSH connection carried in a stream's messages.
type sshConn struct {
	stream  *sshStream
	reader  *io.PipeReader
	sending sync.Mutex
	closed  bool
}

// newSSHConn reads the stream into the connection until the caller closes its side, renewing the
// caller's sign-in with each token it sends.
func newSSHConn(ctx context.Context, stream *sshStream) *sshConn {
	reader, writer := io.Pipe()
	go func() {
		_ = writer.CloseWithError(receiveSSH(ctx, stream, writer))
	}()

	return &sshConn{stream: stream, reader: reader}
}

func receiveSSH(ctx context.Context, stream *sshStream, into io.Writer) error {
	for {
		request, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("failed to hear from the caller: %w", err)
		}

		if token := request.GetToken(); token != "" {
			if err := caller.Renew(ctx, token); err != nil {
				return fmt.Errorf("failed to renew the caller's sign-in: %w", err)
			}
		}

		if _, err := into.Write(request.GetData()); err != nil {
			return fmt.Errorf("failed to hand on the caller's bytes: %w", err)
		}
	}
}

func (c *sshConn) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}

	if err != nil {
		return n, fmt.Errorf("failed to read the SSH connection: %w", err)
	}

	return n, nil
}

func (c *sshConn) Write(p []byte) (int, error) {
	c.sending.Lock()
	defer c.sending.Unlock()

	if c.closed {
		return 0, net.ErrClosed
	}

	if err := c.stream.Send(&terminalv1.SSHResponse{Data: p}); err != nil {
		return 0, fmt.Errorf("failed to write the SSH connection: %w", err)
	}

	return len(p), nil
}

func (c *sshConn) Close() error {
	c.sending.Lock()
	defer c.sending.Unlock()

	c.closed = true

	return c.reader.CloseWithError(net.ErrClosed)
}

func (c *sshConn) LocalAddr() net.Addr {
	return sshAddr{}
}

func (c *sshConn) RemoteAddr() net.Addr {
	return sshAddr{}
}

func (c *sshConn) SetDeadline(time.Time) error {
	return nil
}

func (c *sshConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *sshConn) SetWriteDeadline(time.Time) error {
	return nil
}

type sshAddr struct{}

func (sshAddr) Network() string {
	return "komrad"
}

func (sshAddr) String() string {
	return "komrad"
}
