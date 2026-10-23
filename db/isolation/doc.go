// Package isolation scopes every engine table to the engine that writes it. It derives the row
// level security policy, the trigger that records the writing engine, and the grants from the
// schema, so no migration has to add them. The migrator applies it inside each migration's
// transaction.
package isolation
