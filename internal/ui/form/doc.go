// Package form holds the questions the panel asks a person, rendered over whichever screen is up. A
// form is a value. It builds its fields from the listing and the inspections it is given, and it
// submits by returning requests for the root to apply. A form may open a subform, which is a form
// of its own.
//
// The package also holds the requests that open, refuse and close a form, and the confirm a person
// answers before a worktree is deleted. The root keeps the form host, which runs the open form and
// holds it while its write is in flight.
//
// The root, api, the menu and git menu packages, and the agents, documents, review, schedules and
// worktrees screens import form. It imports only the shared packages below it, and never the root
// or a domain.
package form
