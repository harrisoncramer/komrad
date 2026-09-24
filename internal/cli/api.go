package cli

import (
	"context"
	"fmt"
	"time"

	"komrade/internal/caller"
)

// dialAPI returns the address this machine reaches the engine's api on, and a release for the
// forward it opened.
func (a *app) dialAPI(ctx context.Context) (string, func(), error) {
	release := func() {}

	remote := a.engine()
	if !remote.IsRemote() {
		return a.env.Addr, release, nil
	}

	tunnel, err := remote.Dial(ctx, a.env.Addr, tunnelWait)
	if err != nil {
		return "", release, fmt.Errorf("failed to forward a port to the engine serving %s: %w", a.env.Addr, err)
	}

	return tunnel.Local, func() { _ = tunnel.Close() }, nil
}

// apiCallTimeout bounds one call a command makes to the service.
const apiCallTimeout = 60 * time.Second

// caller is who a call from this process says it is from: the agent this pane belongs to, or
// nobody when the call goes to an engine that has no such agent.
func (a *app) caller() caller.Caller {
	if a.engine().IsRemote() {
		return caller.Caller{}
	}

	return caller.Caller{AgentID: a.env.AgentID, SessionID: a.env.SessionID}
}

// withAPI reaches the service this komrade is pointed at, on this machine or through a forward
// to the engine, and hands the address to run for the length of one command.
func (a *app) withAPI(ctx context.Context, run func(ctx context.Context, base string) error) error {
	ctx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()

	addr, release, err := a.dialAPI(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := requireServer(ctx, addr, a.env.Addr); err != nil {
		return err
	}

	return run(ctx, "http://"+addr)
}
