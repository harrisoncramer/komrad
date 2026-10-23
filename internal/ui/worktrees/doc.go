// Package worktrees holds the screen that lists the directories agents work in, grouped under their
// repositories or flat. A row of scopes narrows the directories listed, and a sort strip orders
// them. The record names a directory's size, age, occupants and git standing, which the root reads
// from an inspection of the directory under the cursor. The block beside the list shows the
// directory's document, and the saved screen of an occupant that has stopped.
//
// The package owns the menus that delete worktrees and the confirms a person answers before they
// go.
//
// The root is the host. It opens the screen and renders its list from the parameters the screen
// hands it. The worktrees screen reaches the host only through the requests it returns, and it
// never imports the root or another domain.
package worktrees
