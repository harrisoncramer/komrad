package doctor

import (
	"context"
	"fmt"
	"net"
	"strconv"
)

const maxPort = 65535

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

type ServerStatus struct {
	Answering bool

	Ready bool

	Version string

	Current bool
}

// Server reports who holds the address the panel talks through, since an address held by
// something else leaves the panel failing to bind and reporting a startup error naming none
// of it.
func Server() Check {
	return Check{
		Name:        "server",
		Description: "the panel reads and changes agents through a local service",
		Run: func(ctx context.Context, env Env) Result {
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

func describeStaleServerFix(ctx context.Context, env Env) string {
	if !isPanelRunning(ctx, env) {
		return "weave stop, then reopen the panel"
	}

	return "tmux -L " + env.TmuxSocket + " kill-server, then reopen the panel, which also restarts every agent"
}

func isPanelRunning(ctx context.Context, env Env) bool {
	if env.TmuxSocket == "" || env.PanelSession == "" {
		return false
	}

	_, err := env.Output(ctx, "tmux", "-L", env.TmuxSocket, "has-session", "-t", env.PanelSession)

	return err == nil
}
