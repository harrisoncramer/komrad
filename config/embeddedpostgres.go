package config

import (
	"net/url"
	"path/filepath"
)

const (
	// EmbeddedPostgresOwner is the superuser initdb creates, which owns komrad's schema.
	EmbeddedPostgresOwner = "komrad"
	// EmbeddedPostgresDatabase is the database komrad keeps its records in.
	EmbeddedPostgresDatabase = "komrad"
	// EmbeddedPostgresPort names the socket file, since the server listens on no TCP address.
	EmbeddedPostgresPort = "5432"
)

// GetEmbeddedPostgresDir is the directory under stateDir that holds the embedded Postgres and its socket.
func GetEmbeddedPostgresDir(stateDir string) string {
	return filepath.Join(stateDir, "postgres")
}

// BuildEmbeddedPostgresURL is the url that reaches database name in the embedded Postgres in dir as
// login, over its socket and without a password.
func BuildEmbeddedPostgresURL(dir, login, name string) string {
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.User(login),
		Path:     "/" + name,
		RawQuery: url.Values{"host": {dir}, "port": {EmbeddedPostgresPort}}.Encode(),
	}).String()
}

// isEmbeddedPostgresURL reports whether databaseURL reaches the embedded Postgres in dir over its socket.
func isEmbeddedPostgresURL(databaseURL, dir string) bool {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return false
	}

	host := parsed.Query().Get("host")

	return host != "" && filepath.Clean(host) == filepath.Clean(dir)
}
