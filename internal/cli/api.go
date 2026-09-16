package cli

import (
	"context"
	"fmt"
)

// dialAPI returns the address this machine reaches the engine's api on, and what to call when it
// is finished with.
func (a *app) dialAPI(ctx context.Context) (string, func(), error) {
	release := func() {}

	remote := a.engine()
	if !remote.IsRemote() {
		return a.env.Addr, release, nil
	}

	tunnel, err := remote.Dial(ctx, a.env.Addr, tunnelWait)
	if err != nil {
		return "", release, fmt.Errorf("forward a port to the engine serving %s: %w", a.env.Addr, err)
	}

	return tunnel.Local, func() { _ = tunnel.Close() }, nil
}
