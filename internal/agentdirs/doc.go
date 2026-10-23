// Package agentdirs checks that a directory on the engine lies inside a live agent's directory,
// after following its links. The git service and remote shells use it to refuse any other path.
package agentdirs
