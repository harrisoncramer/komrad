// Package db embeds komrad's Atlas migrations and trigger files. It also holds the application
// name each pool connects under, the checks for common Postgres errors, and the setup of each
// engine's logins and River schema, which every connection to the database shares.
package db
