package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"weave/config"
	"weave/lib/xslice"
)

const maxNamedOrphans = 3

var (
	ErrHomeUnknown = &Problem{
		Code:    "home_unknown",
		Message: "the home directory the default hooks and worktrees paths hang off could not be read",
	}
	ErrHooksDirAbsent = &Problem{
		Code:    "hooks_dir_absent",
		Message: "there is no hooks directory, and a workflow that names a hook finds none rather than an error",
	}
	ErrHooksDirUnreadable = &Problem{
		Code:    "hooks_dir_unreadable",
		Message: "the hooks directory could not be read",
	}
	ErrHooksDirNotADirectory = &Problem{
		Code:    "hooks_dir_not_a_directory",
		Message: "the hooks directory is not a directory",
	}
	ErrWorktreesDirAbsent = &Problem{
		Code:    "worktrees_dir_absent",
		Message: "the shared worktrees directory is not there",
	}
	ErrWorktreesDirUnreadable = &Problem{
		Code:    "worktrees_dir_unreadable",
		Message: "a worktrees directory could not be read",
	}
	ErrWorktreesDirNotADirectory = &Problem{
		Code:    "worktrees_dir_not_a_directory",
		Message: "the worktrees directory is not a directory",
	}
	ErrWorktreesDirUnwritable = &Problem{
		Code:    "worktrees_dir_unwritable",
		Message: "the worktrees directory cannot be written to, so an agent that needs a worktree has nowhere to be put",
	}
	ErrWorktreesUnowned = &Problem{
		Code:    "worktrees_unowned",
		Message: "there are worktree directories no agent answers for",
	}
	ErrWorktreeOwnersUnreadable = &Problem{
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

// Hooks reports the directory a workflow's hooks are read from, which is asked about here
// because a directory that is not there reads as a machine with no hooks.
func Hooks() Check {
	return Check{
		Name:        "hooks",
		Description: "a workflow hangs the user's own scripts on moments in its agents' lives",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetHooksDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrHomeUnknown, err), "", "")
			}
			shown := describePath(cfg.Hooks.Dir, resolved)

			state, err := env.InspectDir(resolved)
			switch {
			case err != nil:
				return fail(fmt.Errorf("%w: %w", ErrHooksDirUnreadable, err), shown,
					"chmod u+rx "+shown+", or point hooks.dir at a directory you can read")

			case !state.Present:
				return warn(fmt.Errorf("%w: %s", ErrHooksDirAbsent, shown), shown)

			case !state.Directory:
				return fail(fmt.Errorf("%w: %s", ErrHooksDirNotADirectory, shown), shown,
					"move whatever is at that path, or point hooks.dir somewhere else")
			}

			return pass(shown + ", " + describeCount(len(state.Files), "hook", "hooks"))
		},
	}
}

// Worktrees reports the directory a provisioning workflow cuts its worktrees in, since a
// provisioner that cannot make one there refuses the create.
func Worktrees() Check {
	return Check{
		Name:        "worktrees",
		Description: "a workflow that provisions its agents gives each one a worktree here",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			resolved, err := cfg.GetWorktreesDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrHomeUnknown, err), "", "")
			}
			shown := describePath(cfg.Worktrees.Dir, resolved)

			state, err := env.InspectDir(resolved)
			switch {
			case err != nil:
				return fail(fmt.Errorf("%w: %w", ErrWorktreesDirUnreadable, err), shown,
					"chmod u+rx "+shown+", or point worktrees.dir at a directory you can read")

			case !state.Present:
				return warn(fmt.Errorf(
					"%w: %s, which the first agent that needs it creates, and a workflow may name its own instead",
					ErrWorktreesDirAbsent, shown), shown)

			case !state.Directory:
				return fail(fmt.Errorf("%w: %s", ErrWorktreesDirNotADirectory, shown), shown,
					"move whatever is at that path, or point worktrees.dir somewhere else")

			case !state.Writable:
				return fail(fmt.Errorf("%w: %s", ErrWorktreesDirUnwritable, shown), shown,
					"chmod u+w "+shown+", or point worktrees.dir at a directory you own")
			}

			return pass(shown + ", " + describeCount(len(state.Directories), "worktree", "worktrees"))
		},
	}
}

// OrphanedWorktrees reports worktree directories no agent answers for, which nothing else
// notices because a worktree is only removed when the agent holding it is deleted.
func OrphanedWorktrees() Check {
	return Check{
		Name:        "orphaned worktrees",
		Description: "a worktree outlives its agent when the agent's row goes without it",
		Needs:       []string{"config", "database"},
		Run: func(ctx context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			owners, err := env.ReadWorktreeOwners(ctx, env.DatabaseURL)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", ErrWorktreeOwnersUnreadable, err), "")
			}

			shared, err := cfg.GetWorktreesDir()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrHomeUnknown, err), "", "")
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
		return warn(fmt.Errorf("%w: %s%s", ErrWorktreesUnowned,
			strings.Join(described, "; "), describeUnreadableDirs(unreadable)), "")

	case len(unreadable) > 0:
		return warn(fmt.Errorf("%w: %s", ErrWorktreesDirUnreadable, joinWords(unreadable)), "")
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

// describeUnowned names the directory once and the worktrees under it after, since a report
// that repeats the path per worktree is a line nobody reads to the end of.
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
