// Package sshserver serves SSH on the engine over connections the engine's api has already signed
// in, so editors reach the agents' files with no SSH port open.
package sshserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"komrad/internal/logging"
	"komrad/internal/pty"

	gliderssh "github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// hostKeyFile is the file under the state directory that keeps the server's host key.
const hostKeyFile = "ssh_host_ed25519_key"

// NewServerParams is what the server is built over.
type NewServerParams struct {
	Logger *slog.Logger
	// StateDir is where the host key is kept, made on first use.
	StateDir string
	// Shell runs each command, and is the login shell a session without one opens.
	Shell string
}

// Server is an SSH server that runs everything as this process's own account.
type Server struct {
	logger    *slog.Logger
	server    *gliderssh.Server
	publicKey string
	shell     string
}

// NewServer builds the server, reading its host key from the state directory or making one there.
func NewServer(params NewServerParams) (*Server, error) {
	signer, err := loadHostKey(filepath.Join(params.StateDir, hostKeyFile))
	if err != nil {
		return nil, err
	}

	s := &Server{
		logger:    params.Logger,
		publicKey: strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey()))),
		shell:     params.Shell,
	}

	s.server = &gliderssh.Server{
		Handler: s.runSession,
		LocalPortForwardingCallback: func(gliderssh.Context, string, uint32) bool {
			return true
		},
		ChannelHandlers: map[string]gliderssh.ChannelHandler{
			"session":                        gliderssh.DefaultSessionHandler,
			"direct-tcpip":                   gliderssh.DirectTCPIPHandler,
			"direct-streamlocal@openssh.com": forwardToSocket,
		},
	}
	s.server.AddHostKey(signer)

	return s, nil
}

// PublicKey is the host key's public half, in the form a known_hosts line carries it.
func (s *Server) PublicKey() string {
	return s.publicKey
}

// Serve runs one SSH connection over conn until it closes or ctx ends.
func (s *Server) Serve(ctx context.Context, conn net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.server.HandleConn(conn)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		_ = conn.Close() //nolint:errcheck // the sign-in ending is why the connection closes
		<-done
	}
}

func loadHostKey(path string) (gossh.Signer, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is under the engine's own state directory
	if errors.Is(err, os.ErrNotExist) {
		return makeHostKey(path)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read the SSH host key: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the SSH host key at %s: %w", path, err)
	}

	return signer, nil
}

func makeHostKey(path string) (gossh.Signer, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to make an SSH host key: %w", err)
	}

	block, err := gossh.MarshalPrivateKey(private, "")
	if err != nil {
		return nil, fmt.Errorf("failed to encode the SSH host key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("failed to make the directory for the SSH host key: %w", err)
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("failed to write the SSH host key: %w", err)
	}

	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		return nil, fmt.Errorf("failed to use the SSH host key: %w", err)
	}

	return signer, nil
}

// runSession runs the session's command, or a login shell when it names none, and exits with its
// status.
func (s *Server) runSession(session gliderssh.Session) {
	argv := []string{s.shell, "-l"}
	if command := session.RawCommand(); command != "" {
		argv = []string{s.shell, "-c", command}
	}

	code, err := runCommand(session, argv)
	if err != nil {
		s.logger.LogAttrs(session.Context(), slog.LevelWarn, "failed to run an SSH session", logging.Err(err)...)
		_, _ = fmt.Fprintln(session.Stderr(), err) //nolint:errcheck // a client that hung up has no use for the reason
		code = 1
	}

	_ = session.Exit(code) //nolint:errcheck // a client that hung up has no use for the status
}

func runCommand(session gliderssh.Session, argv []string) (int, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("failed to find the home directory: %w", err)
	}

	params := pty.StartParams{
		Argv: argv,
		Dir:  dir,
		Env:  append(os.Environ(), session.Environ()...),
	}

	request, resized, isTerminal := session.Pty()
	if isTerminal {
		params.Size = toSize(request.Window)
		params.Term = request.Term

		return runOnTerminal(session, params, resized)
	}

	return runPiped(session, params)
}

func runOnTerminal(session gliderssh.Session, params pty.StartParams, resized <-chan gliderssh.Window) (int, error) {
	terminal, err := pty.Start(session.Context(), params)
	if err != nil {
		return 0, err
	}
	defer func() { _ = terminal.Close() }() //nolint:errcheck // the command has exited

	go func() {
		for window := range resized {
			_ = terminal.Resize(toSize(window)) //nolint:errcheck // a terminal that closed ends the session on its own
		}
	}()
	go func() {
		_, _ = io.Copy(terminal, session) //nolint:errcheck // the input ends when the client closes it
	}()

	_, _ = io.Copy(session, terminal) //nolint:errcheck // the output ends when the command exits

	return terminal.Wait()
}

func runPiped(session gliderssh.Session, params pty.StartParams) (int, error) {
	command := exec.CommandContext(session.Context(), params.Argv[0], params.Argv[1:]...) //nolint:gosec // the caller is signed in and runs as the agents do
	command.Dir = params.Dir
	command.Env = params.Env
	command.Stdout = session
	command.Stderr = session.Stderr()

	stdin, err := command.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("failed to open the command's input: %w", err)
	}

	go func() {
		_, _ = io.Copy(stdin, session) //nolint:errcheck // the input ends when the client closes it
		_ = stdin.Close()
	}()

	err = command.Run()

	var exited *exec.ExitError
	if errors.As(err, &exited) {
		status, ok := exited.Sys().(syscall.WaitStatus)
		if ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}

		return exited.ExitCode(), nil
	}

	if err != nil {
		return 0, fmt.Errorf("failed to run %s: %w", params.Argv[0], err)
	}

	return 0, nil
}

// socketChannel is the opening of a direct-streamlocal@openssh.com channel.
type socketChannel struct {
	SocketPath string
	Reserved0  string
	Reserved1  uint32
}

// forwardToSocket connects a channel to a unix socket on the engine, the way neovim's remote screen
// reaches a headless neovim.
func forwardToSocket(_ *gliderssh.Server, _ *gossh.ServerConn, channel gossh.NewChannel, ctx gliderssh.Context) {
	var opening socketChannel
	if err := gossh.Unmarshal(channel.ExtraData(), &opening); err != nil {
		_ = channel.Reject(gossh.ConnectionFailed, "failed to read which socket to connect to") //nolint:errcheck // a client that hung up needs no refusal

		return
	}

	var dialer net.Dialer

	socket, err := dialer.DialContext(ctx, "unix", opening.SocketPath)
	if err != nil {
		_ = channel.Reject(gossh.ConnectionFailed, err.Error()) //nolint:errcheck // a client that hung up needs no refusal

		return
	}

	accepted, requests, err := channel.Accept()
	if err != nil {
		_ = socket.Close() //nolint:errcheck // the channel never opened

		return
	}

	go gossh.DiscardRequests(requests)
	go copyThenClose(accepted, socket)
	go copyThenClose(socket, accepted)
}

func copyThenClose(to, from io.ReadWriteCloser) {
	defer func() { _ = to.Close() }()
	defer func() { _ = from.Close() }()

	_, _ = io.Copy(to, from) //nolint:errcheck // either side closing ends the forward
}

func toSize(window gliderssh.Window) pty.Size {
	return pty.Size{
		Rows: uint16(min(window.Height, 65535)), //nolint:gosec // bounded above
		Cols: uint16(min(window.Width, 65535)),  //nolint:gosec // bounded above
	}
}
