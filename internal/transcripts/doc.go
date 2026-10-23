// Package transcripts copies each agent's conversation log into komrad's database and reads it
// back. The syncer reads each harness's log past the last sync, and the reader searches
// transcripts, returns a window of one, lists an agent's newest turns, and forwards turns to
// another agent as mail.
package transcripts
