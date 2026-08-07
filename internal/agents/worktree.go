package agents

import (
	"strings"
)

// worktreeSetupPrefix and worktreeCleanupPrefix name the two scripts a worktree-enabled
// workflow generates, underscore-led so they stay out of the panel's action list per the
// _edit_action convention, and suffixed with the workflow's own id so two worktree-enabled
// workflows never collide over the same file.
const (
	worktreeSetupPrefix   = "_worktree_setup_"
	worktreeCleanupPrefix = "_worktree_cleanup_"
)

// worktreeSetupName is the setup action a worktree-enabled workflow generates for itself.
func worktreeSetupName(workflowID string) string {
	return worktreeSetupPrefix + workflowID
}

// worktreeCleanupName is the cleanup action a worktree-enabled workflow generates for itself.
func worktreeCleanupName(workflowID string) string {
	return worktreeCleanupPrefix + workflowID
}

// worktreeScriptParams is what the two generated scripts are built from.
type worktreeScriptParams struct {
	// Repo is the workflow's cwd, the checkout the worktree is added off.
	Repo string
	// WorktreesDir is where worktrees are created, one subdirectory per agent.
	WorktreesDir string
	// BaseBranch is the branch a new worktree is built from, blank for the repo's default.
	BaseBranch string
	// CopyPaths are files copied fresh into the worktree, relative to Repo.
	CopyPaths []string
	// SymlinkPaths are directories symlinked back to Repo instead of duplicated.
	SymlinkPaths []string
}

// renderWorktreeSetupScript builds the setup action a worktree-enabled workflow points at.
// It fetches the base branch when the repo has a remote, adds the worktree with an existence
// check that makes it a no-op on WEAVE_TRIGGER=resume, and then applies the configured copy
// and symlink entries every run so an edit to one of them reaches a worktree that already
// exists. Only the final path goes to stdout; everything else is redirected to stderr per
// the setup action contract in docs/actions.md.
func renderWorktreeSetupScript(p worktreeScriptParams) string {
	var b strings.Builder

	b.WriteString("#!/bin/sh\n")
	b.WriteString(`#WEAVE description="Generated worktree setup. Edit this workflow's worktree settings instead of this file; it is rewritten whenever they change."` + "\n")
	b.WriteString("set -eu\n\n")
	b.WriteString("repo=" + shellQuote(p.Repo) + "\n")
	b.WriteString("worktrees_dir=" + shellQuote(p.WorktreesDir) + "\n")
	b.WriteString("configured_base=" + shellQuote(p.BaseBranch) + "\n")
	b.WriteString(`tree="$worktrees_dir/$WEAVE_AGENT_ID"` + "\n\n")

	b.WriteString(`if [ -d "$tree" ]; then` + "\n")
	b.WriteString("\t" + `echo "worktree already exists at $tree" >&2` + "\n")
	b.WriteString("else\n")
	b.WriteString("\tbase=\"$configured_base\"\n")
	b.WriteString("\tif [ -z \"$base\" ]; then\n")
	b.WriteString("\t\t" + `base=$(git -C "$repo" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's@^origin/@@') || true` + "\n")
	b.WriteString("\t\tif [ -z \"$base\" ]; then\n")
	b.WriteString("\t\t\tbase=main\n")
	b.WriteString("\t\tfi\n")
	b.WriteString("\tfi\n\n")
	b.WriteString("\tpoint=\"$base\"\n")
	b.WriteString("\tif git -C \"$repo\" remote get-url origin >/dev/null 2>&1; then\n")
	b.WriteString("\t\t" + `echo "fetching $base from origin" >&2` + "\n")
	b.WriteString("\t\tif git -C \"$repo\" fetch --quiet origin \"$base\" >&2; then\n")
	b.WriteString("\t\t\tpoint=\"origin/$base\"\n")
	b.WriteString("\t\tfi\n")
	b.WriteString("\tfi\n\n")
	b.WriteString("\tmkdir -p \"$worktrees_dir\"\n")
	b.WriteString("\t" + `echo "creating a worktree at $tree from $point" >&2` + "\n")
	b.WriteString("\tgit -C \"$repo\" worktree add -b \"agent/$WEAVE_AGENT_ID\" \"$tree\" \"$point\" >&2\n")
	b.WriteString("fi\n\n")

	b.WriteString(renderPathApplyLoop("copy_paths", p.CopyPaths, copyApplyBody))
	b.WriteString(renderPathApplyLoop("symlink_paths", p.SymlinkPaths, symlinkApplyBody))

	b.WriteString(`echo "$tree"` + "\n")

	return b.String()
}

// copyApplyBody and symlinkApplyBody are the loop body for each kind of path entry, run once
// per non-blank line of the block renderPathApplyLoop builds.
const (
	copyApplyBody = `	src="$repo/$p"
	dest="$tree/$p"
	if [ -e "$src" ]; then
		mkdir -p "$(dirname "$dest")"
		cp -f "$src" "$dest"
	else
		echo "worktree setup: $p not found in $repo, skipping copy" >&2
	fi
`
	symlinkApplyBody = `	src="$repo/$p"
	dest="$tree/$p"
	if [ -e "$src" ]; then
		mkdir -p "$(dirname "$dest")"
		ln -sfn "$src" "$dest"
	else
		echo "worktree setup: $p not found in $repo, skipping symlink" >&2
	fi
`
)

// renderPathApplyLoop builds the shell that walks one list of repo-relative paths, applying
// body to each. The list is baked in as one shell-quoted, newline-separated block rather than
// a POSIX sh array, which sh has none of, and is read with `while read` in a subshell whose
// exit is forced to succeed so one bad entry does not abort the rest of setup.
func renderPathApplyLoop(varName string, paths []string, body string) string {
	var b strings.Builder

	b.WriteString(varName + "=" + shellQuote(strings.Join(paths, "\n")) + "\n")
	b.WriteString(`printf '%s\n' "$` + varName + `" | while IFS= read -r p; do` + "\n")
	b.WriteString("\tif [ -z \"$p\" ]; then\n")
	b.WriteString("\t\tcontinue\n")
	b.WriteString("\tfi\n")
	b.WriteString(body)
	b.WriteString("done || true\n\n")

	return b.String()
}

// renderWorktreeCleanupScript builds the cleanup action a worktree-enabled workflow points
// at. Both steps are best-effort, since a workflow's cleanup action must not block a delete
// whatever state the worktree or its branch are already in.
func renderWorktreeCleanupScript(p worktreeScriptParams) string {
	var b strings.Builder

	b.WriteString("#!/bin/sh\n")
	b.WriteString(`#WEAVE description="Generated worktree cleanup. Edit this workflow's worktree settings instead of this file; it is rewritten whenever they change."` + "\n")
	b.WriteString("set -eu\n\n")
	b.WriteString("repo=" + shellQuote(p.Repo) + "\n")
	b.WriteString("worktrees_dir=" + shellQuote(p.WorktreesDir) + "\n")
	b.WriteString(`tree="$worktrees_dir/$WEAVE_AGENT_ID"` + "\n\n")
	b.WriteString(`git -C "$repo" worktree remove --force "$tree" || true` + "\n")
	b.WriteString(`git -C "$repo" branch -D "agent/$WEAVE_AGENT_ID" || true` + "\n")

	return b.String()
}

// shellQuote wraps s as one single-quoted POSIX sh token, safe to splice into generated
// script source whatever it contains, embedded newlines included.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
