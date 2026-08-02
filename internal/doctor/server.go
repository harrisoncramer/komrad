package doctor

import (
	"context"
	"fmt"
	"net"
	"strconv"
)

// maxPort is the highest port there is, so an address above it is refused rather than
// reported as a server that is not answering.
const maxPort = 65535

// The ways the listen address can be wrong.
var (
	ErrServerStale = &Problem{
		Code:    "server_stale",
		Message: "the server holding the address is a different build",
	}
	ErrServerNotReady = &Problem{
		Code:    "server_not_ready",
		Message: "the server is listening but cannot answer",
	}
	ErrAddressTaken = &Problem{
		Code:    "address_taken",
		Message: "something that is not weave is holding the listen address",
	}
	ErrServerDown = &Problem{
		Code:    "server_down",
		Message: "nothing is serving",
	}
	ErrAddressUnusable = &Problem{
		Code:    "address_unusable",
		Message: "WEAVE_ADDR is not an address the server can listen on",
	}
)

// ServerStatus is what the health route said about whoever holds the address.
type ServerStatus struct {
	// Answering is whether anything replied at all.
	Answering bool
	// Ready is whether it replied that it can reach its database, which is a stronger
	// thing than a socket that accepts, since the server binds before it opens the store.
	Ready bool
	// Version is the build it reported, empty when the reply was not weave's health.
	Version string
	// Current is whether that build is this one.
	Current bool
}

// Server reports who holds the address the panel talks through. Nothing serving is the
// ordinary state before the first open, so it is a note. The address held by something else
// is what is worth catching, since the panel then fails to bind and reports a startup error
// naming none of it.
func Server() Check {
	return Check{
		Name:        "server",
		Description: "the panel reads and changes agents through a local service",
		Run: func(ctx context.Context, env Env) Result {
			// Ahead of the probe, since an address nothing can bind answers a probe the
			// same way an address nobody has started yet does.
			if err := requireListenable(env.Addr); err != nil {
				return fail(err, env.Addr, "set WEAVE_ADDR to a host and port, such as 127.0.0.1:8787")
			}

			status := env.ProbeServer(ctx, env.Addr)

			switch {
			case !status.Answering:
				return warn(
					fmt.Errorf("%w at %s, which is normal until the panel is first opened",
						ErrServerDown, env.Addr),
					"",
				)

			case status.Version == "":
				return fail(
					fmt.Errorf("%w: %s answers, but not as weave", ErrAddressTaken, env.Addr),
					env.Addr,
					"free the address, or set WEAVE_ADDR to one nothing else is using",
				)

			case !status.Current:
				return fail(
					fmt.Errorf("%w: %s is serving build %s and this is %s",
						ErrServerStale, env.Addr, status.Version, env.Version),
					env.Addr,
					describeStaleServerFix(ctx, env),
				)

			case !status.Ready:
				// No restart advice. The pool reconnects, so the server recovers on
				// its own and the panel clears within a poll.
				return fail(
					fmt.Errorf("%w: %s is up but reports it cannot reach its database", ErrServerNotReady, env.Addr),
					env.Addr,
					"bring the database back up, and the server will pick it up again",
				)
			}

			return pass(env.Addr + " serving " + status.Version)
		},
	}
}

// requireListenable refuses an address the service could never bind. Go's listener takes a
// service name in place of a number, so only a port that is neither a name nor a usable
// number is refused. Port zero is refused with them, since it binds whatever is free and
// leaves every client looking at the port nobody got.
func requireListenable(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %q has no port", ErrAddressUnusable, addr)
	}

	number, err := strconv.Atoi(port)
	if err != nil {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return fmt.Errorf("%w: %q names no port", ErrAddressUnusable, port)
		}

		return nil
	}

	if number < 1 || number > maxPort {
		return fmt.Errorf("%w: %d is not a port", ErrAddressUnusable, number)
	}

	return nil
}

// describeStaleServerFix is how to get onto the current build, which depends on whether the
// panel is up.
//
// With no panel, stopping the server is the whole fix, since the next open creates the
// session and spawns the build doing the opening. With a panel that advice strands the
// user. The panel runs ensureServer once, when its session is created, so re-opening the
// popup re-attaches the process already there and nothing starts a server again.
//
// What is left costs the agents, because they are windows in the panel's own session. The
// fix says so rather than letting it be discovered.
func describeStaleServerFix(ctx context.Context, env Env) string {
	if !isPanelRunning(ctx, env) {
		return "weave stop, then reopen the panel"
	}

	return "tmux -L " + env.TmuxSocket + " kill-server, then reopen the panel, which also restarts every agent"
}

// isPanelRunning reports whether the panel's tmux session is up. has-session is asked
// rather than anything that would create a server, for the reason the shell check gives.
func isPanelRunning(ctx context.Context, env Env) bool {
	if env.TmuxSocket == "" || env.PanelSession == "" {
		return false
	}

	_, err := env.Output(ctx, "tmux", "-L", env.TmuxSocket, "has-session", "-t", env.PanelSession)

	return err == nil
}
