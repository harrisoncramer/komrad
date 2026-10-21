package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStartsRebase covers which shell command lines start a rebase.
func TestStartsRebase(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "a rebase onto the base branch",
			line: "git rebase origin/main",
			want: true,
		},
		{
			name: "a rebase with git's own options before it",
			line: "git -C /tmp/work -c core.editor=true rebase main",
			want: true,
		},
		{
			name: "a rebase after another command",
			line: "git fetch && git rebase origin/main",
			want: true,
		},
		{
			name: "a rebase in a subshell",
			line: "(cd /tmp/work; git rebase main)",
			want: true,
		},
		{
			name: "a rebase under an environment assignment",
			line: "GIT_EDITOR=true git rebase -i HEAD~2",
			want: true,
		},
		{
			name: "a rebase by full path",
			line: "/usr/bin/git rebase main",
			want: true,
		},
		{
			name: "a rebase inside a command substitution",
			line: "echo $(git rebase main)",
			want: true,
		},
		{
			name: "a pull that rebases",
			line: "git pull --rebase origin main",
			want: true,
		},
		{
			name: "a pull that rebases by its short option",
			line: "git pull -r",
			want: true,
		},
		{
			name: "a pull that rebases keeping merges",
			line: "git pull --rebase=merges",
			want: true,
		},
		{
			name: "a pull that turns the rebase off again",
			line: "git pull --rebase --no-rebase",
		},
		{
			name: "a pull told not to rebase",
			line: "git pull --rebase=false",
		},
		{
			name: "a plain pull",
			line: "git pull",
		},
		{
			name: "a stopped rebase carried on",
			line: "git add -A && GIT_EDITOR=true git rebase --continue",
		},
		{
			name: "a stopped rebase abandoned",
			line: "git rebase --abort",
		},
		{
			name: "the word rebase quoted in a commit message",
			line: `git commit -m "git rebase main"`,
		},
		{
			name: "the word rebase in a search",
			line: "grep -rn rebase internal",
		},
		{
			name: "a log of the rebase",
			line: "git log --grep rebase",
		},
		{
			name: "a line that will not parse",
			line: "git rebase 'main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, startsRebase(tt.line))
		})
	}
}
