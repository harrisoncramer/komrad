package doctor

import (
	"context"
	"fmt"
)

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

// Server reports who holds the address the panel talks through.
//
// Nothing serving is the ordinary state, not a problem: the panel spawns the server the
// first time it opens. What is worth catching is the address held by something else, since
// the panel will then try to start a server, fail to bind, and report a startup error that
// names none of this.
func Server() Check {
	return Check{
		Name:        "server",
		Description: "the panel reads and changes agents through a local service",
		Run: func(ctx context.Context, env Env) Result {
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
					staleServerFix(ctx, env),
				)

			case !status.Ready:
				// No restart advice. The pool reconnects, so the server picks the
				// database back up on its own and the panel clears within a poll.
				return fail(
					fmt.Errorf("%w: %s is up but reports it cannot reach its database", ErrServerNotReady, env.Addr),
					env.Addr,
					"bring the database back up, and the server will pick it up again",
				)
			}

			return ok(env.Addr + " serving " + status.Version)
		},
	}
}

// staleServerFix is how to get onto the current build, which depends on whether the panel
// is up.
//
// With no panel, stopping the server is the whole fix: the next open creates the session
// and spawns the build doing the opening. With a panel, that same advice strands the user.
// The panel runs ensureServer once, when its session is created, so re-opening the popup
// re-attaches the process that is already there and nothing ever starts a server again.
//
// What is left in that case costs the agents. They are windows in the panel's own session,
// so replacing the panel replaces them, and the fix says so rather than finding out later.
func staleServerFix(ctx context.Context, env Env) string {
	if !panelRunning(ctx, env) {
		return "weave stop, then reopen the panel"
	}

	return "tmux -L " + env.TmuxSocket + " kill-server, then reopen the panel, which also restarts every agent"
}

// panelRunning reports whether the panel's tmux session is up. has-session is asked rather
// than anything that would create a server, for the reason the shell check gives.
func panelRunning(ctx context.Context, env Env) bool {
	if env.TmuxSocket == "" || env.PanelSession == "" {
		return false
	}

	_, err := env.Output(ctx, "tmux", "-L", env.TmuxSocket, "has-session", "-t", env.PanelSession)

	return err == nil
}
