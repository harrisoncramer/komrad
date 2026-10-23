// Package agent stores agents. It reads and writes each agent's record, the state komrad keeps
// for it, how its last turn ended and why its create failed. It archives, restores and deletes
// agents in batches, and marks the agents a delete has taken.
//
// A launch locks the engine's agent slots here and counts the live agents against the ceiling.
// A create can be found again by its idempotency key.
package agent
