// Package postgres runs the embedded Postgres under komrad's state directory on a laptop.
package postgres

import (
	"context"
	"net"
	"path/filepath"
	"time"

	"komrad/config"
)

const dialTimeout = time.Second

// IsRunning reports whether the embedded Postgres accepts connections on the socket in dir.
func IsRunning(ctx context.Context, dir string) bool {
	dialer := net.Dialer{Timeout: dialTimeout}

	conn, err := dialer.DialContext(ctx, "unix", getSocketPath(dir))
	if err != nil {
		return false
	}

	return conn.Close() == nil
}

// getSocketPath is the socket the embedded Postgres in dir listens on.
func getSocketPath(dir string) string {
	return filepath.Join(dir, ".s.PGSQL."+config.EmbeddedPostgresPort)
}

// getPIDFilePath is the file the embedded Postgres in dir writes its PID into while it runs.
func getPIDFilePath(dir string) string {
	return filepath.Join(getDataDir(dir), "postmaster.pid")
}

// getLockFilePaths are the files the embedded Postgres in dir leaves behind when it dies without stopping.
func getLockFilePaths(dir string) []string {
	return []string{getPIDFilePath(dir), getSocketPath(dir) + ".lock"}
}
