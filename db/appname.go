package db

import (
	"fmt"
	"os"
)

// appNamePrefix marks a connection as komrade's own in pg_stat_activity, which the migration pass
// reads to tell its own connections from another komrade's.
const appNamePrefix = "komrade"

// maxAppName is what Postgres truncates application_name at, applied here so two processes on a
// long hostname do not end up under one truncated name.
const maxAppName = 63

// AppName is the application_name every komrade pool connects under, naming the process rather
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
