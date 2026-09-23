package db

import (
	"fmt"
	"os"
	"strings"
)

// appNamePrefix marks a connection as weave's own in pg_stat_activity, which the migration pass
// reads to tell its own connections from another weave's.
const appNamePrefix = "weave"

// maxAppName is what Postgres truncates application_name at, applied here so two processes on a
// long hostname do not end up under one truncated name.
const maxAppName = 63

// AppName is the application_name every weave pool connects under, naming the process rather
// than the build so a migration pass can tell which connections are its own.
func AppName() string {
	return appName(hostname(), os.Getpid())
}

// AppHost is the machine AppName names, cut where AppName cuts it.
func AppHost() string {
	return hostOf(AppName())
}

func appName(host string, pid int) string {
	name := fmt.Sprintf("%s/%s/%d", appNamePrefix, host, pid)
	if len(name) > maxAppName {
		name = name[:maxAppName]
	}

	return name
}

func hostOf(name string) string {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 2 {
		return ""
	}

	return parts[1]
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}

	return host
}
