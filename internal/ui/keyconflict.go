package ui

import (
	"fmt"
	"slices"

	"komrad/config"

	"charm.land/bubbles/v2/key"
)

// screenNames are the screens as an error names them.
var screenNames = map[screen]string{
	screenAgents:        "the agents list",
	screenSchedules:     "the schedules",
	screenArchive:       "the archive",
	screenReview:        "the review",
	screenSearch:        "the search",
	screenWorktrees:     "the worktrees",
	screenDocuments:     "the documents",
	screenLogs:          "the logs",
	screenDocumentTypes: "the document types",
}

// dispatchArm is one arm of the order a press is offered to, described for the error it would be
// named in and guarded where it answers only some of the presses that reach it.
type dispatchArm struct {
	describe string
	keys     []string
	guarded  bool
}

// dispatching is a screen's table as the keybinding check walks it.
type dispatching interface {
	dispatchArms(keys config.Keybindings) []dispatchArm
}

// actionsOf is the tables each screen dispatches through, one at a time, walked behind the global
// keys.
func actionsOf(s screen) []dispatching {
	switch s {
	case screenAgents:
		return []dispatching{(&agentsScreen{}).actions()}
	case screenSchedules:
		return []dispatching{(&schedulesScreen{}).actions()}
	case screenArchive:
		return []dispatching{(&archiveScreen{}).actions()}
	case screenReview:
		return []dispatching{(&reviewScreen{}).diffActions(), (&reviewScreen{}).filesActions()}
	case screenWorktrees:
		return []dispatching{(&worktreesScreen{}).actions()}
	case screenDocuments:
		return []dispatching{(&documentsScreen{}).listActions(), (&documentsScreen{}).diffActions(), docActions(), (&documentsScreen{}).docKeys()}
	case screenLogs:
		return []dispatching{(&logsScreen{}).listActions(), (&logsScreen{}).logActions()}
	case screenDocumentTypes:
		types := &documentsScreen{shelf: shelfDocumentTypes}

		return []dispatching{types.listActions(), types.diffActions(), docActions(), types.docKeys()}
	case screenSearch:
		return []dispatching{(&searchScreen{}).listActions(), (&searchScreen{}).previewActions()}
	}

	return nil
}

// ValidateKeybindings rejects the bindings a screen could not dispatch.
func ValidateKeybindings(keys config.Keybindings) error {
	for _, s := range []screen{screenAgents, screenSchedules, screenArchive, screenReview, screenWorktrees, screenDocuments, screenLogs, screenSearch, screenDocumentTypes} {
		if err := validateScreenKeys(s, keys); err != nil {
			return err
		}
	}

	if err := validateSubmenuKeys("git menu", []submenuKeys{
		{name: "rebase", keys: keys.Git.Rebase},
		{name: "open_pull_request", keys: keys.Git.OpenPullRequest},
		{name: "copy_pull_request_url", keys: keys.Git.CopyPullRequestURL},
		{name: "toggle_draft", keys: keys.Git.ToggleDraft},
		{name: "merge", keys: keys.Git.Merge},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("archive menu", []submenuKeys{
		{name: "restore", keys: keys.ArchiveMenu.Restore},
		{name: "restore_open", keys: keys.ArchiveMenu.RestoreOpen},
		{name: "delete", keys: keys.ArchiveMenu.Delete},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("documents menu", []submenuKeys{
		{name: "view", keys: keys.DocumentsMenu.View},
		{name: "new", keys: keys.DocumentsMenu.New},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("document types menu", []submenuKeys{
		{name: "list", keys: keys.DocumentTypesMenu.List},
		{name: "new", keys: keys.DocumentTypesMenu.New},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("merge menu", []submenuKeys{
		{name: "merge", keys: keys.Merge.Merge},
		{name: "merge_archive", keys: keys.Merge.MergeArchive},
	}); err != nil {
		return err
	}

	return validateSubmenuKeys("menu", []submenuKeys{
		{name: "review", keys: keys.Menu.Review},
		{name: "documents", keys: keys.Menu.Documents},
		{name: "logs", keys: keys.Menu.Logs},
		{name: "terminal", keys: keys.Menu.Terminal},
		{name: "git", keys: keys.Menu.Git},
		{name: "archive", keys: keys.Menu.Archive},
		{name: "scratch", keys: keys.Menu.Scratch},
		{name: "document_types", keys: keys.Menu.DocumentTypes},
	})
}

// submenuReserved is every key a submenu is drawn over, named as the error would name it.
func submenuReserved() map[string]string {
	km := submenuKeyMap(formKeys())

	arms := []struct {
		describe string
		binding  key.Binding
	}{
		{describe: "the key that takes a form down", binding: km.Quit},
		{describe: "the key that picks the entry under the cursor", binding: km.Select.Next},
		{describe: "the key that answers a form", binding: km.Select.Submit},
		{describe: "the key that moves up a form's list", binding: km.Select.Up},
		{describe: "the key that moves down a form's list", binding: km.Select.Down},
	}

	reserved := map[string]string{}
	for _, arm := range arms {
		for _, k := range arm.binding.Keys() {
			reserved[k] = arm.describe
		}
	}

	return reserved
}

// submenuKeys is one entry of a submenu as the check names it.
type submenuKeys struct {
	name string
	keys []string
}

// validateSubmenuKeys refuses an entry's key that the submenu around it needs, or that another
// of its entries holds.
func validateSubmenuKeys(menu string, entries []submenuKeys) error {
	reserved := submenuReserved()

	for _, entry := range entries {
		for _, k := range entry.keys {
			if taken, ok := reserved[k]; ok {
				return fmt.Errorf("keybindings: the %s's %s is bound to %q, which is %s, and the menu answers it first",
					menu, entry.name, k, taken)
			}

			reserved[k] = "the menu's " + entry.name + " entry"
		}
	}

	return nil
}

// validateScreenKeys walks each of one screen's tables in dispatch order and finds a key an
// earlier unguarded arm answers ahead of a later one.
func validateScreenKeys(s screen, keys config.Keybindings) error {
	tables := actionsOf(s)
	if len(tables) == 0 {
		return validateArms(s, globalArms(s, keys))
	}

	for _, table := range tables {
		if err := validateArms(s, append(globalArms(s, keys), table.dispatchArms(keys)...)); err != nil {
			return err
		}
	}

	return nil
}

// validateArms finds a key an earlier unguarded arm answers ahead of a later one.
func validateArms(s screen, arms []dispatchArm) error {
	// answered holds, per key, the arm that takes every press of it and the arm's own place in the
	// order, so an arm is not read as shadowing itself.
	type shadow struct {
		describe string
		at       int
	}

	answered := map[string]shadow{}

	for at, arm := range arms {
		for _, key := range arm.keys {
			if taken, ok := answered[key]; ok && taken.at != at {
				if taken.describe == arm.describe {
					return fmt.Errorf("keybindings: on %s, %q is bound to two of the screen's own actions, and the first answers every press of it, so the second never runs",
						screenNames[s], key)
				}

				return fmt.Errorf("keybindings: on %s, %q is bound to both %s and %s, and %s answers every press of it, so the other never runs",
					screenNames[s], key, taken.describe, arm.describe, taken.describe)
			}

			if !arm.guarded {
				answered[key] = shadow{describe: arm.describe, at: at}
			}
		}
	}

	return nil
}

// globalArms are the keys the panel answers on one screen ahead of that screen's own table, in
// the order it reads them.
func globalArms(s screen, keys config.Keybindings) []dispatchArm {
	arms := []dispatchArm{{describe: "the leader", keys: []string{leaderKey}}}
	if s.picksViews() {
		arms = append(arms, dispatchArm{describe: "the view switcher", keys: slices.Concat(keys.Views.Next, keys.Views.Previous)})
	}

	arms = append(arms,
		dispatchArm{describe: "the key that reads the messages", keys: keys.Messages.Open},
		dispatchArm{describe: "the help toggle", keys: keys.Help.Toggle},
		dispatchArm{describe: "the sidebar narrowing", keys: keys.Sidebar.Narrow},
		dispatchArm{describe: "the sidebar widening", keys: keys.Sidebar.Widen},
	)

	var zero model

	if _, ok := zero.screenOf(s).(tabbed); ok {
		arms = append(arms, dispatchArm{describe: "the tabs focus", keys: keys.Tabs.Focus})
	}

	if _, ok := zero.screenOf(s).(kinded); ok {
		arms = append(arms, dispatchArm{describe: "the kinds focus", keys: keys.Kinds.Focus})
	}

	arms = append(arms, dispatchArm{describe: "the menu", keys: keys.Menu.Open})

	if _, ok := zero.screenOf(s).(detailed); ok {
		arms = append(arms,
			// Both are read ahead of the screen's own keys.
			dispatchArm{describe: "the key that pushes a fixture into the messages", keys: []string{messagesMockKey}},
			// The copy key answers only while the details hold the cursor.
			dispatchArm{
				describe: "the details copy",
				keys:     keys.Details.Copy,
				guarded:  true,
			},
		)
	}

	return arms
}
