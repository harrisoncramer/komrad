package cli

import "weave/internal/server"

// sameBuild reports whether a server that answered with this health body is running this code.
func sameBuild(body server.Health, selfVersion, selfCommit string) bool {
	if selfCommit != "" || body.Commit != "" {
		return selfCommit == body.Commit
	}

	return body.Version == selfVersion
}
