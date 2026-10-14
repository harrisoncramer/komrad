package engine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"komrad/lib/clock"
)

// dialTimeout is how long one attempt to reach the forwarded port may take.
const dialTimeout = time.Second

// pollInterval is how often komrad tries the forwarded port while the forward is still setting it up.
const pollInterval = 100 * time.Millisecond

// redialDelay is how long a tunnel whose forward has exited waits before running it again.
const redialDelay = time.Second

// probeInterval is how often komrad checks that a tunnel still answers, whether its own forward
// carries it or one somebody else runs.
const probeInterval = 5 * time.Second

// silentProbesBeforeRedial is how many probes in a row may go unanswered before komrad ends its own
// forward and runs it again, so one slow round trip does not drop every stream through it.
const silentProbesBeforeRedial = 3

// probeRequest is what komrad writes to a port to learn whether the engine's api is behind it.
const probeRequest = "HEAD / HTTP/1.0\r\n\r\n"

// tunnelPortBase and tunnelPortSpan are the range where komrad places an engine's forward, high enough
// to be out of the way and narrow enough to stay recognisable.
const (
	tunnelPortBase = 19000
	tunnelPortSpan = 1000
)

// Tunnel is a forward from a port on this machine to the engine's api.
type Tunnel struct {
	// Local is the address on this machine that now reaches the engine.
	Local string
	argv  []string
	wait  time.Duration
	mu    sync.Mutex
	// forward is the run of the command this tunnel owns, nil while it is borrowing a forward
	// that was already up when it was dialled.
	forward *forward
	closed  bool
}

// forward is one run of the command carrying the tunnel.
type forward struct {
	command *exec.Cmd
	exited  chan struct{}
}

// TunnelPort is the port an engine's forward always lands on, derived from the engine so that two
// of them never collide and a tunnel outliving its panel is reused rather than leaked.
func (r Remote) TunnelPort(engineAddr string) int {
	sum := sha256.Sum256([]byte(r.Destination + strings.Join(r.Forward, " ") + "/" + engineAddr))

	return tunnelPortBase + int(binary.BigEndian.Uint16(sum[:2]))%tunnelPortSpan
}

// TunnelArgv is what is run to forward local to the engine's address.
func (r Remote) TunnelArgv(local, engineAddr string) []string {
	if len(r.Forward) > 0 {
		_, localPort, _ := net.SplitHostPort(local)       //nolint:errcheck // komrad built this address
		_, enginePort, _ := net.SplitHostPort(engineAddr) //nolint:errcheck // a bad one fails at dial

		return fill(r.Forward, map[string]string{
			LocalPortPlaceholder:  localPort,
			remotePortPlaceholder: enginePort,
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

// Dial forwards a port on this machine to the address the engine serves its api on and keeps the
// forward running until Close.
func (r Remote) Dial(ctx context.Context, engineAddr string, wait time.Duration) (*Tunnel, error) {
	if !r.IsRemote() {
		return nil, fmt.Errorf("the engine is this machine, so there is nothing to tunnel to")
	}
	if r.URL != "" {
		return nil, fmt.Errorf("the engine is reached at %s, so there is nothing to tunnel to", r.URL)
	}

	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(r.TunnelPort(engineAddr)))

	tunnel := &Tunnel{
		Local: local,
		argv:  r.TunnelArgv(local, engineAddr),
		wait:  wait,
	}

	if !isAnswering(ctx, local) {
		if err := tunnel.start(ctx); err != nil {
			return nil, fmt.Errorf("failed to carry %s through the tunnel to %s: %w", engineAddr, r, err)
		}
	}

	go tunnel.keep(ctx)

	return tunnel, nil
}

// start runs the forward and waits for the engine to answer through it.
func (t *Tunnel) start(ctx context.Context) error {
	// The forward deliberately stays in the panel's process group, so killing the panel
	// outright takes its forward with it rather than leaving a forward nobody owns.
	command := exec.CommandContext(ctx, t.argv[0], t.argv[1:]...) //nolint:gosec // the destination is the user's own config

	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to run %s: %w", t.argv[0], err)
	}

	running := &forward{command: command, exited: make(chan struct{})}

	go func() {
		_ = command.Wait() //nolint:errcheck // what the forward exited with is not the question
		close(running.exited)
	}()

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		running.kill()

		return errors.New("the tunnel is closed")
	}

	t.forward = running
	t.mu.Unlock()

	if err := t.waitForAnswer(ctx, running); err != nil {
		running.kill()

		return err
	}

	return nil
}

// waitForAnswer blocks until the engine answers through the forward, or the forward goes first.
func (t *Tunnel) waitForAnswer(ctx context.Context, running *forward) error {
	deadline := time.Now().Add(t.wait)

	for {
		if running.isExited() {
			return fmt.Errorf("%s exited before carrying %s", t.argv[0], t.Local)
		}

		if isAnswering(ctx, t.Local) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("nothing answered on %s within %s", t.Local, t.wait)
		}

		clock.Sleep(ctx, pollInterval)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// keep runs the forward again each time it exits or stops answering, and falls back to watching the
// port when the port turns out to be somebody else's.
func (t *Tunnel) keep(ctx context.Context) {
	for {
		t.mu.Lock()
		running, closed := t.forward, t.closed
		t.mu.Unlock()

		if closed {
			return
		}

		if running == nil {
			clock.Sleep(ctx, probeInterval)
			if ctx.Err() != nil {
				return
			}

			if isAnswering(ctx, t.Local) {
				continue
			}
		} else {
			t.watch(ctx, running)
			if ctx.Err() != nil {
				return
			}

			if isAnswering(ctx, t.Local) {
				t.mu.Lock()
				t.forward = nil
				t.mu.Unlock()

				continue
			}

			clock.Sleep(ctx, redialDelay)
			if ctx.Err() != nil {
				return
			}
		}

		if t.isClosed() {
			return
		}

		if err := t.start(ctx); err != nil {
			t.mu.Lock()
			t.forward = nil
			t.mu.Unlock()
		}
	}
}

// watch blocks until the forward exits or the context ends, ending the forward first when the engine
// stops answering through it for silentProbesBeforeRedial probes in a row.
func (t *Tunnel) watch(ctx context.Context, running *forward) {
	silent := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-running.exited:
			return
		case <-time.After(probeInterval):
		}

		if isAnswering(ctx, t.Local) {
			silent = 0

			continue
		}

		silent++
		if silent >= silentProbesBeforeRedial {
			running.kill()

			return
		}
	}
}

func (t *Tunnel) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}

// Close takes the tunnel down, leaving a borrowed forward to whoever runs it.
func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true

	if t.forward != nil {
		t.forward.kill()
	}

	return nil
}

func (f *forward) isExited() bool {
	select {
	case <-f.exited:
		return true
	default:
		return false
	}
}

// kill ends the forward and reaps it, treating one already gone as done.
func (f *forward) kill() {
	_ = f.command.Process.Kill() //nolint:errcheck // one already gone is the outcome asked for
	<-f.exited
}

// isAnswering reports whether something behind the port answers a request.
func isAnswering(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: dialTimeout}

	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	defer func() { _ = connection.Close() }() //nolint:errcheck // the answer is the whole question

	_ = connection.SetDeadline(time.Now().Add(dialTimeout)) //nolint:errcheck // a connection just dialled takes one

	if _, err := connection.Write([]byte(probeRequest)); err != nil {
		return false
	}

	read, err := connection.Read(make([]byte, 1))

	return err == nil && read > 0
}
