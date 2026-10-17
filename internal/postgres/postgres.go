// Package postgres runs the Postgres komrad keeps under its state directory on a laptop.
package postgres

import (
	"context"
	"net"
	"path/filepath"
	"time"
)

const (
	// port names the socket file, since the server listens on no TCP address.
	port        = "5432"
	dialTimeout = time.Second
)

// GetDir is the directory under stateDir that holds the embedded Postgres and its socket.
func GetDir(stateDir string) string {
	return filepath.Join(stateDir, "postgres")
}

// IsRunning reports whether a Postgres server accepts connections on the socket in dir.
func IsRunning(ctx context.Context, dir string) bool {
	dialer := net.Dialer{Timeout: dialTimeout}

	conn, err := dialer.DialContext(ctx, "unix", getSocketPath(dir))
	if err != nil {
		return false
	}

	return conn.Close() == nil
}

// getSocketPath is the socket the server in dir listens on.
func getSocketPath(dir string) string {
	return filepath.Join(dir, ".s.PGSQL."+port)
}

// getPIDFilePath is the file the server in dir writes its PID into while it runs.
func getPIDFilePath(dir string) string {
	return filepath.Join(getDataDir(dir), "postmaster.pid")
}

// getLockFilePaths are the files a server in dir leaves behind when it dies without stopping.
func getLockFilePaths(dir string) []string {
	return []string{getPIDFilePath(dir), getSocketPath(dir) + ".lock"}
}
