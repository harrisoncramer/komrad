// Package watch is the coordinator between the panel's rows and the database that holds them,
// read in one direction: listen, pending set, read back by key, render, decide, fan out.
//
// Four words are worth keeping apart. State is what is true now, and Postgres holds it. A
// notification is Postgres saying a row moved, carrying the table and the key and nothing else,
// ephemeral and never authoritative on its own. An event in the durable sense, an append-only
// log that can be replayed, is what weave deliberately does not keep. Reconciliation is how a
// watcher converges on the state anyway: a snapshot on connect and another after every break,
// so a notification that never arrives costs a reread rather than correctness.
//
// The Broker holds one connection of its own on the watch channel, coalesces what it hears into
// the pending set, reads those rows back and sends each Subscription the row or, for a key its
// watched set no longer holds, a tombstone. Every Subscription is dropped when that connection
// is lost, so a watcher's picture is never older than the last change it could have heard.
//
// The triggers carry no opinion about whether a write is worth anyone's attention. The broker
// compares each row it reads back against the last one it sent for that key, so a write that
// moves only a stamp sends nothing.
package watch
