package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestQuoteFlagsNeutralisesShellSyntax covers the injection this quoting exists
// to stop. Flags reach the pane through send-keys, so anything a shell would act
// on has to come out the far side as a literal argument.
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
			// A shell would treat this unterminated quote as a syntax error. The
			// splitter simply consumes it, which yields one plain argument.
			name:  "an unterminated quote is consumed as a delimiter",
			flags: `--model o'pus`,
			want:  `'--model' 'opus'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quoteFlags(tt.flags))
		})
	}
}

// TestLaunchClaudeQuotesFlags confirms the quoting is actually applied on the
// path that types into the pane, not just available to it.
func TestLaunchClaudeQuotesFlags(t *testing.T) {
	tmux := newFakeTmux()
	manager := newManager(tmux, newStore(t), "")

	require.NoError(t, manager.launcher.launch(launchClaudeParams{
		Session:   "session",
		Flags:     "--model $(touch /tmp/pwned)",
		SessionID: "sess",
	}))

	command := tmux.lastKeys("session")[0]
	assert.NotContains(t, command, "claude --model $(", "an unquoted substitution would run in the pane")
	assert.Contains(t, command, `'$(touch'`, "the substitution survives only as a literal argument")
}

// TestQuoteArgEscapesEmbeddedQuote covers the one case single quoting cannot
// nest: the argument's own quote has to close, escape, and reopen.
func TestQuoteArgEscapesEmbeddedQuote(t *testing.T) {
	assert.Equal(t, `'o'\''pus'`, quoteArg(`o'pus`))
}
