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
	errServerStale = &ProblemError{
		Code:    "server_stale",
		Message: "build mismatch",
	}
	errServerNotReady = &ProblemError{
		Code:    "server_not_ready",
		Message: "the server is listening but cannot answer",
	}
	errAddressTaken = &ProblemError{
		Code:    "address_taken",
		Message: "something that is not weave is holding the listen address",
	}
	errServerDown = &ProblemError{
		Code:    "server_down",
		Message: "nothing is serving",
	}
	errAddressUnusable = &ProblemError{
		Code:    "address_unusable",
		Message: "WEAVE_ADDR is not an address the server can listen on",
	}
	errEngineUnreachable = &ProblemError{
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
					fmt.Errorf("%w: %w", errEngineUnreachable, err),
					env.Addr,
					"check the engine is up and that this engine's ssh settings reach it",
				)
			}

			switch {
			case !status.Answering && env.EngineIsRemote:
				return fail(
					fmt.Errorf("%w: the engine answers nothing on %s", errServerDown, env.Addr),
					env.Addr,
					"start weave on the engine, since its service runs whether or not a panel is open",
				)

			case !status.Answering:
				return warn(
					fmt.Errorf("%w at %s, which is normal until the panel is first opened",
						errServerDown, env.Addr),
					"",
				)

			case status.Version == "":
				return fail(
					fmt.Errorf("%w: %s answers, but not as weave", errAddressTaken, env.Addr),
					env.Addr,
					describeTakenAddressFix(env),
				)

			case !status.Current:
				return fail(
					fmt.Errorf("%w: %s %s, this weave %s",
						errServerStale, describeStaleSide(env),
						version.Describe(status.Version, status.Commit),
						version.Describe(env.Version, env.Commit)),
					env.Addr,
					describeStaleServerFix(ctx, env, status.Version, status.Commit),
				)

			case !status.Ready:

				return fail(
					fmt.Errorf("%w: %s is up but reports it cannot reach its database", errServerNotReady, env.Addr),
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

	return "free the address, or set WEAVE_ADDR to one nothing else is using"
}

func describeStaleSide(env Env) string {
	if env.EngineIsRemote {
		return "engine"
	}

	return "server"
}

func describeStaleServerFix(ctx context.Context, env Env, engineVersion, engineCommit string) string {
	if env.EngineIsRemote {
		return StaleEngineFix(engineVersion, engineCommit)
	}

	if !env.IsPanelRunning(ctx) {
		return "weave stop, then reopen the panel"
	}

	return "tmux -L " + env.TmuxSocket + " kill-server, then reopen the panel, which also restarts every agent"
}

// StaleEngineFix says how to bring this binary to the build a remote engine is serving.
func StaleEngineFix(engineVersion, engineCommit string) string {
	switch {
	case engineCommit == "":
		return "use the dist/weave-panel that deploy built, since it has no commit to build from"
	case version.IsEngineStamp(engineVersion):
		return "build weave from " + version.Short(engineCommit) + ", or put this build on the engine with mise run remote:deploy"
	default:
		return "install weave " + engineVersion + ", which the engine runs"
	}
}
