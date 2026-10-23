// Package agent serves AgentService, the RPCs that create, read and change agents with their
// mail, transcripts, documents and reviews. It also plans and runs the push and rebase of a task's
// worktree, undoes a sequence left part-way through it, and posts the person's replies to the forge.
//
// Each method parses the request, scopes it to the caller where the method calls for that, and
// calls the service that owns the rule. It renders the result through the render package, and a
// domain error reaches the caller with the code its kind names.
package agent
