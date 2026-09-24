// Package agents holds the lifecycle of an agent: what it means to launch one, resume one,
// archive one, or adopt work that already exists.
//
// It sits between the handlers, which only parse and translate, and the stores, which only
// read and write rows. The rules that need more than one of those live here. Launching an
// agent, for instance, is a check that the workflow has room under the ceiling, a provisioner
// run to completion, a tmux window created, a harness asked for its command line, and a row
// written, and none of those layers can decide the order on its own.
//
// The reconcile loops are here too, and they are the reason komrade keeps working when something
// goes wrong outside it. Nothing tells komrade that an agent stopped thinking or that its pane
// died. Each loop reads what is actually true, compares it against the rows, and closes the
// gap, so a missed event costs one pass rather than a stuck agent.
package agents
