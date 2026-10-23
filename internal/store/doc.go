// Package store opens komrad's connection to Postgres and runs the transactions the domain
// stores share. Each domain keeps its queries in a package of its own under internal/store.
//
// A [Pool] connects as one engine's login, and the database scopes every read and write to that
// engine. [Open] refuses a schema that lacks a migration this build carries, and the pool never
// migrates. [Migrate] and [EnsureEngine] connect as the database owner instead. [OpenReadOnly]
// opens the engine's reader login for queries that must never write.
//
// A write that spans domains runs inside [Pool.InTx], and each domain store takes the
// transaction as its [DBTX]. The package also grants and revokes a person's access to an engine,
// and [AccessStore] checks that access when the person signs in.
package store
