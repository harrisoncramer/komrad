// Package db embeds komrad's Atlas migrations and trigger files, and holds what every connection
// to the database shares. That covers the application name each pool connects under, the checks
// for common Postgres errors, and the setup of each engine's logins and River schema.
package db
