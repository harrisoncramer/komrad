// Package agents holds the agent lifecycle: launching, resuming, restarting, archiving and
// deleting agents, starting a workflow's orchestrator, adopting a branch komrad did not make, and
// spawning a reviewer when an agent asks for one.
//
// The handlers parse requests and the stores read and write, and this package holds the rules
// that span them. A launch checks the workflow's ceilings, records the agent, runs the
// provisioner and the task's hooks, and starts the harness in a pane with the agent's briefing.
// When a step fails, the launch gives back the checkout it provisioned.
//
// The [Reconciler] runs here too. Nothing tells komrad that an agent stopped working or that its
// pane died, so each pass reads every pane, compares what it sees with the agent's recorded
// state, and corrects the difference. A missed signal costs one pass instead of a stuck agent.
package agents
