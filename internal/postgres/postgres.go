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

// Dir is the directory under stateDir that holds komrad's Postgres and its socket.
func Dir(stateDir string) string {
	return filepath.Join(stateDir, "postgres")
}

// IsRunning reports whether a Postgres server accepts connections on the socket in dir.
func IsRunning(ctx context.Context, dir string) bool {
	dialer := net.Dialer{Timeout: dialTimeout}

	conn, err := dialer.DialContext(ctx, "unix", socketPath(dir))
	if err != nil {
		return false
	}

	return conn.Close() == nil
}

// socketPath is the socket the server in dir listens on.
func socketPath(dir string) string {
	return filepath.Join(dir, ".s.PGSQL."+port)
}
