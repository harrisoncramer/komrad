package cli

import (
	"context"
	"fmt"

	"komrad/internal/doctor"
	"komrad/internal/errui"
	"komrad/internal/server"
	"komrad/internal/version"
)

// isSameBuild reports whether a server that answered with this health body is running this code.
func isSameBuild(body server.Health, selfVersion, selfCommit string) bool {
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

	return fmt.Errorf("%w: engine %s, this komrad %s; %s",
		errui.ErrEngineBuild,
		version.Describe(probe.version, probe.commit),
		version.Describe(version.Version(), version.Commit()),
		doctor.StaleEngineFix(probe.version, probe.commit))
}
