package doctor

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"komrad/internal/version"
)

const maxPort = 65535

var (
	errServerNotReady = &problemError{
		Code:    "server_not_ready",
		Message: "the server is listening but cannot answer",
	}
	errAddressTaken = &problemError{
		Code:    "address_taken",
		Message: "something that is not komrad is holding the listen address",
	}
	errServerDown = &problemError{
		Code:    "server_down",
		Message: "nothing is serving",
	}
	errAddressUnusable = &problemError{
		Code:    "address_unusable",
		Message: "KOMRAD_ADDR is not an address the server can listen on",
	}
	errEngineUnreachable = &problemError{
		Code:    "engine_unreachable",
		Message: "the engine could not be reached from this machine",
	}
)

type ServerStatus struct {
	Answering bool
	Ready     bool
	Version   string
	Commit    string
}

// newServerCheck reports who holds the address the panel talks through.
func newServerCheck() Check {
	return Check{
		Name:        CheckServer,
		Description: "the panel reads and changes agents through a local server",
		Run: func(ctx context.Context, env Env) Result {
			served := env.EngineURL
			if served == "" {
				if err := requireListenable(ctx, env.Addr); err != nil {
					return fail(err, env.Addr, "set KOMRAD_ADDR to a host and port, such as 127.0.0.1:8787")
				}

				served = env.Addr
			}

			status, err := env.ProbeServer(ctx)
			if err != nil {
				return fail(
					fmt.Errorf("%w: %w", errEngineUnreachable, err),
					served,
					"check the engine is up and that this engine's url reaches it",
				)
			}

			switch {
			case !status.Answering && env.EngineIsRemote:
				return fail(
					fmt.Errorf("%w: the engine answers nothing on %s", errServerDown, served),
					served,
					"start komrad on the engine, since its server runs whether or not a panel is open",
				)

			case !status.Answering:
				return warn(
					fmt.Errorf("%w at %s, which is normal until the panel is first opened",
						errServerDown, served),
					"",
				)

			case status.Version == "":
				return fail(
					fmt.Errorf("%w: %s answers, but not as komrad", errAddressTaken, served),
					served,
					describeTakenAddressFix(env),
				)

			case !status.Ready:

				return fail(
					fmt.Errorf("%w: %s is up but reports it cannot reach its database", errServerNotReady, served),
					served,
					"bring the database back up, and the server will pick it up again",
				)
			}

			return pass(served + " serving " + version.Describe(status.Version, status.Commit))
		},
	}
}

func requireListenable(ctx context.Context, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %q has no port", errAddressUnusable, addr)
	}

	number, err := strconv.Atoi(port)
	if err != nil {
		if _, err := net.DefaultResolver.LookupPort(ctx, "tcp", port); err != nil {
			return fmt.Errorf("%w: %q names no port", errAddressUnusable, port)
		}

		return nil
	}

	if number < 1 || number > maxPort {
		return fmt.Errorf("%w: %d is not a port", errAddressUnusable, number)
	}

	return nil
}

func describeTakenAddressFix(env Env) string {
	if env.EngineIsRemote {
		return "free the address on the engine, since what holds it is a process there"
	}

	return "free the address, or set KOMRAD_ADDR to one nothing else is using"
}
