// Package agents holds weave's business logic: what it means to launch an agent, archive one,
// deliver mail, fire a script, run a schedule, or fold a conversation into the search index.
//
// It sits between the handlers, which only parse and translate, and the stores, which only
// read and write rows. The rules that need more than one of those live here. Launching an
// agent, for instance, is a check that the workflow has room under the ceiling, a provisioner
// run to completion, a tmux window created, a harness asked for its command line, and a row
// written, and none of those layers can decide the order on its own.
//
// The reconcile loops are here too, and they are the reason weave keeps working when something
// goes wrong outside it. Nothing tells weave that an agent stopped thinking or that its pane
// died. Each loop reads what is actually true, compares it against the rows, and closes the
// gap, so a missed event costs one pass rather than a stuck agent.
package agents
