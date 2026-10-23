package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actionsOf is the tables each screen dispatches through, one at a time, walked behind the global
// keys.
func actionsOf(s screen.ID) []keys.Table {
	var zero model
	up := zero.screenOf(s)
	tables := up.Dispatches()
	if d, ok := up.(docKeyer); ok {
		tables = append(tables, zero.docActions(), d.DocKeys(screen.Inputs{}))
	}

	return tables
}

// ValidateKeybindings rejects the bindings a screen could not dispatch.
func ValidateKeybindings(keys config.Keybindings) error {
	for _, s := range screen.IDs {
		if err := validateScreenKeys(s, keys); err != nil {
			return err
		}
	}

	if err := validateSubmenuKeys("git menu", []submenuKeys{
		{name: "rebase", keys: keys.Git.Rebase},
		{name: "push", keys: keys.Git.Push},
		{name: "abort", keys: keys.Git.Abort},
		{name: "forge", keys: keys.Git.Forge},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("archive menu", []submenuKeys{
		{name: "archive", keys: keys.ArchiveMenu.Archive},
		{name: "restore", keys: keys.ArchiveMenu.Restore},
		{name: "restore_open", keys: keys.ArchiveMenu.RestoreOpen},
		{name: "delete", keys: keys.ArchiveMenu.Delete},
		{name: "clear", keys: keys.ArchiveMenu.Clear},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("hide menu", []submenuKeys{
		{name: "hide", keys: keys.HideMenu.Hide},
		{name: "show_hidden", keys: keys.HideMenu.ShowHidden},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("document types menu", []submenuKeys{
		{name: "edit", keys: keys.DocumentTypesMenu.Edit},
		{name: "new", keys: keys.DocumentTypesMenu.New},
		{name: "delete", keys: keys.DocumentTypesMenu.Delete},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("document menu", []submenuKeys{
		{name: "edit", keys: keys.DocumentMenu.Edit},
		{name: "rename", keys: keys.DocumentMenu.Rename},
		{name: "new", keys: keys.DocumentMenu.New},
		{name: "delete", keys: keys.DocumentMenu.Delete},
		{name: "jump", keys: keys.Menu.Jump},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("worktrees menu", []submenuKeys{
		{name: "archive", keys: keys.Menu.Archive},
		{name: "jump", keys: keys.Menu.Jump},
		{name: "clear", keys: keys.ArchiveMenu.Clear},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("schedule menu", []submenuKeys{
		{name: "toggle", keys: keys.ScheduleMenu.Toggle},
		{name: "edit", keys: keys.ScheduleMenu.Edit},
		{name: "new", keys: keys.ScheduleMenu.New},
		{name: "archive", keys: keys.Menu.Archive},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("review menu", []submenuKeys{
		{name: "edit_file", keys: keys.Review.EditFile},
		{name: "expand", keys: keys.Diff.Expand},
		{name: "wrap", keys: keys.Diff.Wrap},
		{name: "reload", keys: keys.Review.Reload},
		{name: "agent_threads", keys: keys.Review.AgentThreads},
		{name: "toggle_viewed", keys: keys.Review.ToggleViewed},
		{name: "compose", keys: keys.Review.Compose},
		{name: "forge_reply", keys: keys.Review.ForgeReply},
		{name: "resolve", keys: keys.Review.Resolve},
		{name: "accept", keys: keys.Review.Accept},
		{name: "forge_post", keys: keys.Review.ForgePost},
		{name: "forge_discard", keys: keys.Review.ForgeDiscard},
		{name: "answer", keys: keys.Review.Answer},
		{name: "jump", keys: keys.Review.Jump},
		{name: "git", keys: keys.Menu.Git},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("jump menu", []submenuKeys{
		{name: "agent", keys: keys.JumpMenu.Agent},
		{name: "task", keys: keys.JumpMenu.Task},
		{name: "worktree", keys: keys.JumpMenu.Worktree},
		{name: "documents", keys: keys.JumpMenu.Documents},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("forge menu", []submenuKeys{
		{name: "open_pull_request", keys: keys.Forge.OpenPullRequest},
		{name: "copy_pull_request_url", keys: keys.Forge.CopyPullRequestURL},
		{name: "edit", keys: keys.Forge.Edit},
		{name: "toggle_draft", keys: keys.Forge.ToggleDraft},
		{name: "request_rereview", keys: keys.Forge.RequestRereview},
		{name: "merge", keys: keys.Forge.Merge},
		{name: "merge_archive", keys: keys.Forge.MergeArchive},
		{name: "auto_merge", keys: keys.Forge.AutoMerge},
		{name: "merge_rebase", keys: keys.Forge.MergeRebase},
	}); err != nil {
		return err
	}

	if err := validateSubmenuKeys("menu", []submenuKeys{
		{name: "review", keys: keys.Menu.Review},
		{name: "logs", keys: keys.Menu.Logs},
		{name: "status checks", keys: keys.Menu.StatusChecks},
		{name: "terminal", keys: keys.Menu.Terminal},
		{name: "edit", keys: keys.Menu.Edit},
		{name: "retry", keys: keys.Menu.Retry},
		{name: "snooze", keys: keys.Menu.Snooze},
		{name: "git", keys: keys.Menu.Git},
		{name: "jump", keys: keys.Menu.Jump},
		{name: "archive", keys: keys.Menu.Archive},
		{name: "hide", keys: keys.Menu.Hide},
		{name: "edit_row", keys: keys.Menu.EditRow},
		{name: "adopt", keys: keys.Menu.Adopt},
		{name: "new_agent", keys: keys.Menu.NewAgent},
		{name: "new_workflow", keys: keys.Menu.NewWorkflow},
		{name: "restore", keys: keys.Menu.Restore},
		{name: "delete", keys: keys.Menu.Delete},
	}); err != nil {
		return err
	}

	return validateSubmenuKeys("task menu", []submenuKeys{
		{name: "review", keys: keys.Menu.Review},
		{name: "logs", keys: keys.Menu.Logs},
		{name: "status checks", keys: keys.Menu.StatusChecks},
		{name: "terminal", keys: keys.Menu.Terminal},
		{name: "new_agent", keys: keys.Menu.NewAgent},
		{name: "edit_row", keys: keys.Menu.EditRow},
		{name: "restart", keys: keys.Menu.Restart},
		{name: "edit", keys: keys.Menu.Edit},
		{name: "snooze", keys: keys.Menu.Snooze},
		{name: "git", keys: keys.Menu.Git},
		{name: "jump", keys: keys.Menu.Jump},
		{name: "hide", keys: keys.Menu.Hide},
		{name: "archive", keys: keys.Menu.Archive},
		{name: "new_workflow", keys: keys.Menu.NewWorkflow},
	})
}

// submenuReserved is every key a submenu is rendered over, named as the error would name it.
func submenuReserved() map[string]string {
	km := submenuKeyMap(fixedKeys())

	arms := []struct {
		describe string
		keys     []string
	}{
		{describe: "the key that takes a form down", keys: km.Quit.Keys()},
		{describe: "the key that picks the entry under the cursor", keys: slices.Concat(km.Select.Submit.Keys(), km.Select.Next.Keys())},
		{describe: "the key that moves up a form's list", keys: km.Select.Up.Keys()},
		{describe: "the key that moves down a form's list", keys: km.Select.Down.Keys()},
	}

	reserved := map[string]string{}
	for _, arm := range arms {
		for _, k := range arm.keys {
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
func validateScreenKeys(s screen.ID, bindings config.Keybindings) error {
	tables := actionsOf(s)
	if len(tables) == 0 {
		return validateArms(s, globalArms(s, bindings))
	}

	for _, table := range tables {
		if err := validateArms(s, append(globalArms(s, bindings), table.Arms(bindings)...)); err != nil {
			return err
		}
	}

	return nil
}

// validateArms finds a key an earlier unguarded arm answers ahead of a later one.
func validateArms(s screen.ID, arms []keys.Arm) error {
	// answered holds, per key, the arm that takes every press of it and the arm's own place in the
	// order, so an arm is not read as shadowing itself.
	type shadow struct {
		describe string
		at       int
	}

	answered := map[string]shadow{}

	for at, arm := range arms {
		for _, key := range arm.Keys {
			if taken, ok := answered[key]; ok && taken.at != at {
				if taken.describe == arm.Describe {
					return fmt.Errorf("keybindings: on %s, %q is bound to two of the screen's own actions, and the first answers every press of it, so the second never runs",
						s, key)
				}

				return fmt.Errorf("keybindings: on %s, %q is bound to both %s and %s, and %s answers every press of it, so the other never runs",
					s, key, taken.describe, arm.Describe, taken.describe)
			}

			if !arm.IsGuarded {
				answered[key] = shadow{describe: arm.Describe, at: at}
			}
		}
	}

	return nil
}
