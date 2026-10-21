package cli

import (
	"path"
	"slices"
	"strings"

	"komrad/internal/harness/claude"
	"komrad/lib/xslice"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

const rebaseRefusal = "Your task is stacked on its parent, and komrad rebases the stack itself. It moves your " +
	"commits onto the parent's newest commit whenever the parent moves, and onto the base branch once the parent " +
	"merges. A rebase by hand can drop the commit your own work starts from, and komrad then stops carrying your " +
	"branch. Leave the rebase to komrad, and tell whoever asked for it that komrad rebases the stack."

// gitValueOptions are git's own options that take the next word as their value.
var gitValueOptions = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env"}

// rebaseResumes are the options that carry on with a rebase already stopped rather than start one.
var rebaseResumes = []string{"--continue", "--abort", "--skip", "--quit", "--edit-todo", "--show-current-patch"}

// wrappers are commands that run the command named after them.
var wrappers = []string{"command", "env", "exec", "nohup", "sudo"}

var rebaseDenial = hookOutput{HookSpecificOutput: hookContext{
	HookEventName:            string(claude.EventPreToolUse),
	PermissionDecision:       "deny",
	PermissionDecisionReason: rebaseRefusal,
}}

// refusesRebase reports whether a tool call starts a rebase on a task whose stack komrad rebases.
func refusesRebase(cmd *cobra.Command, state *app, payload []byte) bool {
	command, err := claude.ReadShellCommand(payload)
	if err != nil || !startsRebase(command) {
		return false
	}

	row, err := readOwnRow(cmd.Context(), state)
	if err != nil {
		reportHookProblem(cmd, state, "komrad could not be reached, so the rebase was let through", err)

		return false
	}

	parent := row.GetTask().GetParent()

	return parent != nil && parent.GetBlocked() != taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED
}

// startsRebase reports whether a shell command line starts a git rebase, or a git pull that rebases.
func startsRebase(line string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(line), "")
	if err != nil {
		return false
	}

	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok && rebases(gitArgs(literalWords(call.Args))) {
			found = true
		}

		return !found
	})

	return found
}

func literalWords(words []*syntax.Word) []string {
	return xslice.Map(words, func(word *syntax.Word) string {
		text, err := expand.Literal(nil, word)
		if err != nil {
			return ""
		}

		return text
	})
}

// gitArgs is what follows git's own options in a command's words, nil when the command does not run git.
func gitArgs(words []string) []string {
	for len(words) > 0 && (slices.Contains(wrappers, words[0]) || strings.Contains(words[0], "=")) {
		words = words[1:]
	}
	if len(words) == 0 || path.Base(words[0]) != "git" {
		return nil
	}

	words = words[1:]
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		if slices.Contains(gitValueOptions, words[0]) && len(words) > 1 {
			words = words[1:]
		}
		words = words[1:]
	}

	return words
}

func rebases(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch args[0] {
	case "rebase":
		return !slices.ContainsFunc(args[1:], func(arg string) bool {
			return slices.Contains(rebaseResumes, arg)
		})
	case "pull":
		rebase := false
		for _, arg := range args[1:] {
			switch {
			case arg == "--no-rebase" || arg == "--rebase=false":
				rebase = false
			case arg == "--rebase" || arg == "-r" || strings.HasPrefix(arg, "--rebase="):
				rebase = true
			}
		}

		return rebase
	default:
		return false
	}
}
