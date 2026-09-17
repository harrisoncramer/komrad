package doctor

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"weave/internal/version"
)

const maxPort = 65535

var (
	ErrServerStale = &ProblemError{
		Code:    "server_stale",
		Message: "the server holding the address is a different build",
	}
	ErrServerNotReady = &ProblemError{
		Code:    "server_not_ready",
		Message: "the server is listening but cannot answer",
	}
	ErrAddressTaken = &ProblemError{
		Code:    "address_taken",
		Message: "something that is not weave is holding the listen address",
	}
	ErrServerDown = &ProblemError{
		Code:    "server_down",
		Message: "nothing is serving",
	}
	ErrAddressUnusable = &ProblemError{
		Code:    "address_unusable",
		Message: "WEAVE_ADDR is not an address the server can listen on",
	}
	ErrEngineUnreachable = &ProblemError{
		Code:    "engine_unreachable",
		Message: "the engine could not be reached from this machine",
	}
)

type ServerStatus struct {
	Answering bool
	Ready     bool
	Version   string
	Commit    string
	Current   bool
}

// newServerCheck reports who holds the address the panel talks through.
func newServerCheck() Check {
	return Check{
		Name:        "server",
		Description: "the panel reads and changes agents through a local service",
		Run: func(ctx context.Context, env Env) Result {
			if err := requireListenable(ctx, env.Addr); err != nil {
				return fail(err, env.Addr, "set WEAVE_ADDR to a host and port, such as 127.0.0.1:8787")
			}

			status, err := env.ProbeServer(ctx)
			if err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrEngineUnreachable, err),
					env.Addr,
					"check the engine is up and that this engine's ssh settings reach it",
				)
			}

			switch {
			case !status.Answering && env.EngineIsRemote:
				return fail(
					fmt.Errorf("%w: the engine answers nothing on %s", ErrServerDown, env.Addr),
					env.Addr,
					"start weave on the engine, since its service runs whether or not a panel is open",
				)

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
					describeTakenAddressFix(env),
				)

			case !status.Current:
				return fail(
					fmt.Errorf("%w: %s is serving build %s and this is %s",
						ErrServerStale, env.Addr,
						version.Describe(status.Version, status.Commit),
						version.Describe(env.Version, env.Commit)),
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

			return pass(env.Addr + " serving " + version.Describe(status.Version, status.Commit))
		},
	}
}

func requireListenable(ctx context.Context, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %q has no port", ErrAddressUnusable, addr)
	}

	number, err := strconv.Atoi(port)
	if err != nil {
		if _, err := net.DefaultResolver.LookupPort(ctx, "tcp", port); err != nil {
			return fmt.Errorf("%w: %q names no port", ErrAddressUnusable, port)
		}

		return nil
	}

	if number < 1 || number > maxPort {
		return fmt.Errorf("%w: %d is not a port", ErrAddressUnusable, number)
	}

	return nil
}

func describeTakenAddressFix(env Env) string {
	if env.EngineIsRemote {
		return "free the address on the engine, since what holds it is a process there"
	}

	return "free the address, or set WEAVE_ADDR to one nothing else is using"
}

func describeStaleServerFix(ctx context.Context, env Env) string {
	if env.EngineIsRemote {
		return "rebuild this panel from the commit the engine is serving, since CI deploys the engine from main"
	}

	if !env.IsPanelRunning(ctx) {
		return "weave stop, then reopen the panel"
	}

	return "tmux -L " + env.TmuxSocket + " kill-server, then reopen the panel, which also restarts every agent"
}
