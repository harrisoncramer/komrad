package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/lib/xslice"
)

const maxNamedOrphans = 3

var (
	errHomeUnknown = &ProblemError{
		Code:    "home_unknown",
		Message: "the home directory the default hooks and worktrees paths hang off could not be read",
	}
	errHooksDirAbsent = &ProblemError{
		Code:    "hooks_dir_absent",
		Message: "there is no hooks directory, and a workflow that names a hook finds none rather than an error",
	}
	errHooksDirUnreadable = &ProblemError{
		Code:    "hooks_dir_unreadable",
		Message: "the hooks directory could not be read",
	}
	errHooksDirNotADirectory = &ProblemError{
		Code:    "hooks_dir_not_a_directory",
		Message: "the hooks directory is not a directory",
	}
	errWorktreesDirAbsent = &ProblemError{
		Code:    "worktrees_dir_absent",
		Message: "the shared worktrees directory is not there",
	}
	errWorktreesDirUnreadable = &ProblemError{
		Code:    "worktrees_dir_unreadable",
		Message: "a worktrees directory could not be read",
	}
	errWorktreesDirNotADirectory = &ProblemError{
		Code:    "worktrees_dir_not_a_directory",
		Message: "the worktrees directory is not a directory",
	}
	errWorktreesDirUnwritable = &ProblemError{
		Code:    "worktrees_dir_unwritable",
		Message: "the worktrees directory cannot be written to, so an agent that needs a worktree has nowhere to be put",
	}
	errWorktreesUnowned = &ProblemError{
		Code:    "worktrees_unowned",
		Message: "there are worktree directories no agent answers for",
	}
	errWorktreeOwnersUnreadable = &ProblemError{
		Code:    "worktree_owners_unreadable",
		Message: "the agents that own the worktrees could not be read",
	}
)

type DirState struct {
	Present     bool
	Directory   bool
	Writable    bool
	Files       []string
	Directories []string
}

type WorktreeOwners struct {
	AgentIDs     []string
	AgentDirs    []string
	WorkflowDirs []string
}

// newHooksCheck reports the directory a workflow's hooks are read from.
func newHooksCheck() Check {
	return Check{
		Name:        "hooks",
		Description: "a workflow hangs the user's own scripts on moments in its agents' lives",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetHooksDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errHomeUnknown, err), "", "")
			}
			shown := describePath(cfg.Hooks.Dir, resolved)

			state, err := env.InspectDir(resolved)
			switch {
			case err != nil:
				return fail(fmt.Errorf("%w: %w", errHooksDirUnreadable, err), shown,
					"chmod u+rx "+shown+", or point hooks.dir at a directory you can read")

			case !state.Present:
				return warn(fmt.Errorf("%w: %s", errHooksDirAbsent, shown), shown)

			case !state.Directory:
				return fail(fmt.Errorf("%w: %s", errHooksDirNotADirectory, shown), shown,
					"move whatever is at that path, or point hooks.dir somewhere else")
			}

			return pass(shown + ", " + describeCount(len(state.Files), "hook", "hooks"))
		},
	}
}

// newWorktreesCheck reports the directory a provisioning workflow cuts its worktrees in.
func newWorktreesCheck() Check {
	return Check{
		Name:        "worktrees",
		Description: "a workflow that provisions its agents gives each one a worktree here",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetWorktreesDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errHomeUnknown, err), "", "")
			}
			shown := describePath(cfg.Worktrees.Dir, resolved)

			state, err := env.InspectDir(resolved)
			switch {
			case err != nil:
				return fail(fmt.Errorf("%w: %w", errWorktreesDirUnreadable, err), shown,
					"chmod u+rx "+shown+", or point worktrees.dir at a directory you can read")

			case !state.Present:
				return warn(fmt.Errorf(
					"%w: %s, which the first agent that needs it creates, and a workflow may name its own instead",
					errWorktreesDirAbsent, shown), shown)

			case !state.Directory:
				return fail(fmt.Errorf("%w: %s", errWorktreesDirNotADirectory, shown), shown,
					"move whatever is at that path, or point worktrees.dir somewhere else")

			case !state.Writable:
				return fail(fmt.Errorf("%w: %s", errWorktreesDirUnwritable, shown), shown,
					"chmod u+w "+shown+", or point worktrees.dir at a directory you own")
			}

			return pass(shown + ", " + describeCount(len(state.Directories), "worktree", "worktrees"))
		},
	}
}

// newOrphanedWorktreesCheck reports worktree directories no agent answers for.
func newOrphanedWorktreesCheck() Check {
	return Check{
		Name:        "orphaned worktrees",
		Description: "a worktree outlives its agent when the agent's row goes without it",
		Needs:       []string{"config", "database"},
		Run: func(ctx context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			owners, err := env.ReadWorktreeOwners(ctx, env.DatabaseURL)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errWorktreeOwnersUnreadable, err), "")
			}

			shared, err := cfg.GetWorktreesDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errHomeUnknown, err), "", "")
			}

			return reportOrphans(env, owners, collectWorktreeDirs(cfg.Worktrees.Dir, shared, owners.WorkflowDirs))
		},
	}
}

type worktreeDir struct {
	shown    string
	resolved string
}

func reportOrphans(env Env, owners WorktreeOwners, dirs []worktreeDir) Result {
	ids := toSet(owners.AgentIDs)
	held := toSet(owners.AgentDirs)

	var described, unreadable []string
	checked := 0

	for _, dir := range dirs {
		state, err := env.InspectDir(dir.resolved)
		if err != nil {
			unreadable = append(unreadable, dir.shown)

			continue
		}
		if !state.Present || !state.Directory {
			continue
		}

		var unowned []string
		for _, name := range state.Directories {
			checked++
			if ids[name] || held[filepath.Join(dir.resolved, name)] {
				continue
			}

			unowned = append(unowned, name)
		}

		if len(unowned) > 0 {
			described = append(described, describeUnowned(dir.shown, unowned))
		}
	}

	switch {
	case len(described) > 0:
		return warn(fmt.Errorf("%w: %s%s", errWorktreesUnowned,
			strings.Join(described, "; "), describeUnreadableDirs(unreadable)), "")

	case len(unreadable) > 0:
		return warn(fmt.Errorf("%w: %s", errWorktreesDirUnreadable, joinWords(unreadable)), "")
	}

	return pass(describeCount(checked, "worktree", "worktrees") + ", every one owned")
}

func collectWorktreeDirs(typed, shared string, workflowDirs []string) []worktreeDir {
	dirs := []worktreeDir{{shown: describePath(typed, shared), resolved: shared}}

	for _, dir := range workflowDirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}

		dirs = append(dirs, worktreeDir{shown: trimmed, resolved: config.ExpandHome(trimmed)})
	}

	return xslice.UniqueBy(dirs, func(dir worktreeDir) string { return dir.resolved })
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}

	return set
}

func describePath(typed, resolved string) string {
	if trimmed := strings.TrimSpace(typed); trimmed != "" {
		return trimmed
	}

	return resolved
}

// describeUnowned names the directory once and the worktrees under it after.
func describeUnowned(dir string, unowned []string) string {
	named, tail := unowned, ""
	if len(unowned) > maxNamedOrphans {
		named = unowned[:maxNamedOrphans]
		tail = " and " + strconv.Itoa(len(unowned)-maxNamedOrphans) + " more"
	}

	return describeCount(len(unowned), "worktree", "worktrees") + " in " + dir +
		": " + strings.Join(named, ", ") + tail
}

func describeUnreadableDirs(unreadable []string) string {
	if len(unreadable) == 0 {
		return ""
	}

	return "; " + joinWords(unreadable) + " could not be read"
}

func describeCount(count int, singular, plural string) string {
	switch count {
	case 0:
		return "no " + plural
	case 1:
		return "1 " + singular
	default:
		return strconv.Itoa(count) + " " + plural
	}
}
