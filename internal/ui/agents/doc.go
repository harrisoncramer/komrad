// Package agents holds the panel's agents list, the screen a person starts on, and the archive of
// agents that have stopped.
//
// The list draws each workflow with its tasks and their agents under it. The triage zone sits on
// top and holds the agents waiting on a person. The package owns the keys, menus and forms that
// act on those records. These cover creating, adopting, renaming, hiding, archiving and deleting
// agents, tasks and workflows, and the subpages menu that opens an agent's logs, review, terminal
// and documents. The archive shows the archived and the deleted agents as two more kinds of the
// same list.
//
// The root package is the host. It routes keys and listings to whichever screen is up, runs the
// writes each screen asks for, and puts panes on the stage. The agents screen reaches the host
// only through the requests it returns, and it never imports the root or another domain. The
// root calls a few methods of its own on the screen, to land on a row a write made, to switch
// between the live and archived kinds, and to read the pane under the cursor for the stage.
package agents
