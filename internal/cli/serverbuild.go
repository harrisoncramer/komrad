package cli

import (
	"context"
	"fmt"

	"weave/internal/server"
	"weave/internal/version"
)

// sameBuild reports whether a server that answered with this health body is running this code.
func sameBuild(body server.Health, selfVersion, selfCommit string) bool {
	if selfCommit != "" || body.Commit != "" {
		return selfCommit == body.Commit
	}

	return body.Version == selfVersion
}

// requireEngineBuild fails when the engine answering on this address is running other code.
func requireEngineBuild(ctx context.Context, addr string) error {
	probe := probeServer(ctx, addr)
	if !probe.answering || probe.version == "" || probe.current {
		return nil
	}

	return fmt.Errorf("the engine is serving build %s and this panel is %s, so rebuild this panel from the commit the engine is serving",
		version.Describe(probe.version, probe.commit),
		version.Describe(version.Version(), version.Commit()))
}
