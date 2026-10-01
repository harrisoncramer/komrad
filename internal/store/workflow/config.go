package workflow

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/fail"
)

var ErrWorktreeUnsafePath = fail.InvalidArgument("a worktree copy or symlink path must be a relative path inside the repo")

var ErrUnknownEventPrompt = fail.InvalidArgument("a prompt must name an event komrad sends")

var ErrDuplicateEventPrompt = fail.InvalidArgument("a workflow holds one prompt per event")

var ErrPromptTooLong = fail.InvalidArgument(fmt.Sprintf("a prompt runs to at most %d characters", eventprompts.MaxPrompt))

// Normalize trims what a caller sent and stores the shortest spelling of each worktree path.
func (c *Config) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Cwd = config.ExpandHome(strings.TrimSpace(c.Cwd))
	c.Flags = strings.TrimSpace(c.Flags)
	c.ReviewerHarness = strings.TrimSpace(c.ReviewerHarness)
	c.ReviewerFlags = strings.TrimSpace(c.ReviewerFlags)
	c.Worktree.BaseBranch = strings.TrimSpace(c.Worktree.BaseBranch)
	c.Worktree.Dir = strings.TrimSpace(c.Worktree.Dir)
	c.Worktree.CopyPaths = cleanPaths(c.Worktree.CopyPaths)
	c.Worktree.SymlinkPaths = cleanPaths(c.Worktree.SymlinkPaths)
	c.Worktree.DiffExclude = cleanPaths(c.Worktree.DiffExclude)
}

// NormalizeEventPrompts trims what a caller sent, drops the blank entries, and refuses an event komrad
// does not send or one named twice.
func (c *Config) NormalizeEventPrompts() error {
	kept := make([]EventPrompt, 0, len(c.EventPrompts))
	for _, prompt := range c.EventPrompts {
		prompt.Event = strings.TrimSpace(prompt.Event)
		prompt.Prompt = strings.TrimSpace(prompt.Prompt)
		if prompt.Prompt == "" {
			continue
		}
		if !eventprompts.IsKnown(prompt.Event) {
			return fmt.Errorf("%w: %q is not one of %s", ErrUnknownEventPrompt, prompt.Event, strings.Join(eventprompts.Names(), ", "))
		}
		if slices.ContainsFunc(kept, func(k EventPrompt) bool { return k.Event == prompt.Event }) {
			return fmt.Errorf("%w: %s", ErrDuplicateEventPrompt, prompt.Event)
		}
		if utf8.RuneCountInString(prompt.Prompt) > eventprompts.MaxPrompt {
			return fmt.Errorf("%w: %s runs to %d characters", ErrPromptTooLong, prompt.Event, utf8.RuneCountInString(prompt.Prompt))
		}

		kept = append(kept, prompt)
	}
	c.EventPrompts = kept

	return nil
}

// ValidatePaths refuses a copy, symlink or diff-exclude path that is not a relative path inside the repo.
func (w WorktreeConfig) ValidatePaths() error {
	for _, list := range [][]string{w.CopyPaths, w.SymlinkPaths, w.DiffExclude} {
		for _, path := range list {
			if err := validateWorktreePath(path); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanPaths drops the blank entries and stores the shortest spelling of the rest.
func cleanPaths(paths []string) []string {
	if paths == nil {
		return nil
	}

	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}

		kept = append(kept, filepath.Clean(trimmed))
	}

	return kept
}

func validateWorktreePath(path string) error {
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("%w: %q holds a line break, which is what separates the entries", ErrWorktreeUnsafePath, path)
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return fmt.Errorf("%w: %q is not relative to the repo", ErrWorktreeUnsafePath, path)
	}

	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == ".." {
			return fmt.Errorf("%w: %q climbs out of the repo", ErrWorktreeUnsafePath, path)
		}
		if segment == "." {
			return fmt.Errorf("%w: %q is the repo rather than something in it", ErrWorktreeUnsafePath, path)
		}
	}

	if strings.EqualFold(segments[0], ".git") {
		return fmt.Errorf("%w: %q is the worktree's own link to the repo", ErrWorktreeUnsafePath, path)
	}

	return nil
}
