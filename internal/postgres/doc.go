// Package postgres runs the embedded Postgres that a build made with the postgres tag carries. It
// unpacks the binaries, creates the cluster and komrad's database under the state directory, and
// starts the server again when it exits.
package postgres
