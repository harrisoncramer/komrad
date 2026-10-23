// Package migrator brings a database's schema up to date from the migrations and trigger files db
// embeds, so a deployment migrates by running the komrad binary without the Atlas CLI.
//
// The [Migrator] applies pending migrations in filename order under an advisory lock, so two
// processes never apply the same one. Each table's triggers live in one versioned file, and a pass
// reapplies a file whose version is newer than the one the database recorded.
// [Migrator.CheckSchema] refuses a database missing a migration or trigger version this build
// carries, and accepts one holding newer versions, so an older build still runs against a newer
// schema.
package migrator
