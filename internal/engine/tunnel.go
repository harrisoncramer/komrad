package engine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// dialTimeout is how long one attempt to reach the forwarded port may take.
const dialTimeout = time.Second

// pollInterval is how often the forwarded port is tried while ssh is still setting it up.
const pollInterval = 100 * time.Millisecond

// tunnelPortBase and tunnelPortSpan are the range an engine's forward is placed in, high enough
// to be out of the way and narrow enough to stay recognisable.
const (
	tunnelPortBase = 19000
	tunnelPortSpan = 1000
)

// Tunnel is an ssh forward from a port on this machine to the engine's api.
type Tunnel struct {
	// Local is the address on this machine that now reaches the engine.
	Local   string
	command *exec.Cmd
	// borrowed says the tunnel was already up when this panel started, so closing it is not this
	// panel's business.
	borrowed bool
}

// TunnelPort is the port an engine's forward always lands on, derived from the engine so that two
// of them never collide and a tunnel outliving its panel is reused rather than leaked.
func (r Remote) TunnelPort(engineAddr string) int {
	sum := sha256.Sum256([]byte(r.Destination + strings.Join(r.Exec, " ") + "/" + engineAddr))

	return tunnelPortBase + int(binary.BigEndian.Uint16(sum[:2]))%tunnelPortSpan
}

// TunnelArgv is what is run to forward local to the engine's address.
func (r Remote) TunnelArgv(local, engineAddr string) []string {
	if len(r.Forward) > 0 {
		_, localPort, _ := net.SplitHostPort(local)       //nolint:errcheck // weave built this address
		_, enginePort, _ := net.SplitHostPort(engineAddr) //nolint:errcheck // a bad one fails at dial

		return fill(r.Forward, map[string]string{
			LocalPortPlaceholder:  localPort,
			RemotePortPlaceholder: enginePort,
		})
	}

	return []string{
		"ssh",
		"-N",
		"-L", local + ":" + engineAddr,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		r.Destination,
	}
}

// Dial forwards a port on this machine to the address the engine serves its api on, so the panel
// can talk to it as though it were local.
func (r Remote) Dial(ctx context.Context, engineAddr string, wait time.Duration) (*Tunnel, error) {
	if !r.IsRemote() {
		return nil, fmt.Errorf("the engine is this machine, so there is nothing to tunnel to")
	}

	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(r.TunnelPort(engineAddr)))

	if err := waitForPort(ctx, local, 0); err == nil {
		return &Tunnel{Local: local, borrowed: true}, nil
	}

	argv := r.TunnelArgv(local, engineAddr)
	// The tunnel is deliberately left in the panel's process group, so a panel that is killed
	// outright takes its ssh with it rather than leaving a forward nobody owns.
	command := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the destination is the user's own config

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("failed to open a tunnel to %s: %w", r, err)
	}

	tunnel := &Tunnel{Local: local, command: command}

	if err := waitForPort(ctx, local, wait); err != nil {
		_ = tunnel.Close() //nolint:errcheck // the start failed, and the close is only tidying up after it

		return nil, fmt.Errorf("the tunnel to %s never carried %s: %w", r, engineAddr, err)
	}

	return tunnel, nil
}

// Close takes the tunnel down, which the panel does on its way out so no ssh is left behind.
func (t *Tunnel) Close() error {
	if t == nil || t.borrowed || t.command == nil || t.command.Process == nil {
		return nil
	}

	if err := t.command.Process.Kill(); err != nil {
		return fmt.Errorf("failed to close the ssh tunnel: %w", err)
	}

	_ = t.command.Wait() //nolint:errcheck // a killed process always reports one

	return nil
}

// waitForPort blocks until the forward is carrying traffic.
func waitForPort(ctx context.Context, address string, wait time.Duration) error {
	deadline := time.Now().Add(wait)

	for {
		dialer := net.Dialer{Timeout: dialTimeout}

		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = connection.Close() //nolint:errcheck // the dial was the whole question

			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("nothing answered on %s within %s: %w", address, wait, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
