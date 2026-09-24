package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSplitArgs pins how a flag string is broken into argv, quoting and empty input
// included.
func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		want  []string
	}{
		{
			name:  "empty",
			flags: "   ",
			want:  nil,
		},
		{
			name:  "plain flags",
			flags: "--model opus",
			want:  []string{"--model", "opus"},
		},
		{
			name:  "extra whitespace collapses",
			flags: "  --model   opus  ",
			want:  []string{"--model", "opus"},
		},
		{
			name:  "a double-quoted value stays one argument",
			flags: `--append-system-prompt "be terse and precise"`,
			want:  []string{"--append-system-prompt", "be terse and precise"},
		},
		{
			name:  "a single-quoted value stays one argument",
			flags: `--dir 'my folder'`,
			want:  []string{"--dir", "my folder"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, splitArgs(tt.flags))
		})
	}
}

// TestQuoteFlagsNeutralisesShellSyntax covers flags reaching the pane through send-keys, where
// anything a shell would act on has to arrive as a literal argument.
func TestQuoteFlagsNeutralisesShellSyntax(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		want  string
	}{
		{
			name:  "command substitution",
			flags: "--model $(echo pwned > /tmp/pwned.txt)",
			want:  `'--model' '$(echo' 'pwned' '>' '/tmp/pwned.txt)'`,
		},
		{
			name:  "a chained command",
			flags: "--model opus; rm -rf /tmp/x",
			want:  `'--model' 'opus;' 'rm' '-rf' '/tmp/x'`,
		},
		{
			name:  "backticks",
			flags: "--model `whoami`",
			want:  "'--model' '`whoami`'",
		},
		{
			name:  "an unterminated quote is consumed as a delimiter",
			flags: `--model o'pus`,
			want:  `'--model' 'opus'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QuoteFlags(tt.flags))
		})
	}
}

// TestQuoteArg pins how an argument is wrapped for the shell, including one holding a quote
// of its own.
func TestQuoteArg(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "a plain argument is wrapped as it stands",
			arg:  "opus",
			want: `'opus'`,
		},
		{
			name: "an embedded quote closes, escapes, and reopens",
			arg:  `o'pus`,
			want: `'o'\''pus'`,
		},
		{
			name: "shell syntax inside the argument is inert once wrapped",
			arg:  "$(whoami)",
			want: `'$(whoami)'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QuoteArg(tt.arg))
		})
	}
}

// TestRequirePOSIXShell pins which login shells komrade can launch an agent under and which it
// refuses.
func TestRequirePOSIXShell(t *testing.T) {
	tests := []struct {
		name  string
		shell string
		want  bool
	}{
		{
			name:  "a shell tmux does not report",
			shell: "",
			want:  true,
		},
		{
			name:  "sh",
			shell: "/bin/sh",
			want:  true,
		},
		{
			name:  "zsh",
			shell: "/bin/zsh",
			want:  true,
		},
		{
			name:  "a shell matched on its basename rather than its path",
			shell: "/opt/homebrew/bin/bash",
			want:  true,
		},
		{
			name:  "fish",
			shell: "/usr/local/bin/fish",
			want:  false,
		},
		{
			name:  "nushell",
			shell: "/opt/homebrew/bin/nu",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequirePOSIXShell(tt.shell)
			if tt.want {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrShellUnsupported)
			assert.Contains(t, err.Error(), tt.shell)
		})
	}
}
