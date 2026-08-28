package db

import (
	"fmt"
	"os"
)

// appNamePrefix marks a connection as weave's own in pg_stat_activity, where the
// migration pass reads it to tell a connection of its own from one belonging to
// another weave against the same database.
const appNamePrefix = "weave"

// maxAppName is what Postgres truncates application_name at, applied here so two
// processes on a long hostname do not end up under the same truncated name.
const maxAppName = 63

// AppName is the application_name every weave pool connects under, naming the process rather
// than the build so a migration pass can tell which connections are its own.
func AppName() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	name := fmt.Sprintf("%s/%s/%d", appNamePrefix, host, os.Getpid())
	if len(name) > maxAppName {
		name = name[:maxAppName]
	}

	return name
}
