package ui

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogArgv covers the shell command a log viewer is run as.
func TestLogArgv(t *testing.T) {
	tests := []struct {
		name        string
		argv        []string
		wantViewer  string
		wantExports []string
		wantAbsent  string
	}{
		{
			name:        "nothing configured falls back to a pager on the file",
			argv:        logArgv("", "/tmp/weave.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/weave.log'; "},
			wantAbsent:  "export WEAVE_AGENT_ID",
		},
		{
			name:        "a viewer that is only whitespace is no viewer at all",
			argv:        logArgv("   ", "/tmp/weave.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/weave.log'; "},
		},
		{
			name:       "a pipeline is left exactly as it was written",
			argv:       logArgv(`jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`, "/tmp/weave.log", "a1"),
			wantViewer: `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
			wantExports: []string{
				"export WEAVE_LOG='/tmp/weave.log'; ",
				"export WEAVE_AGENT_ID='a1'; ",
			},
		},
		{
			name:        "a path holding spaces and quotes survives the export",
			argv:        logArgv("", "/Users/a b/it's.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{`export WEAVE_LOG='/Users/a b/it'\''s.log'`},
		},
		{
			name:        "a process log falls back to its own viewer and names no agent",
			argv:        processLogArgv("", "/tmp/setup.log"),
			wantViewer:  config.DefaultProcessViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/setup.log'; "},
			wantAbsent:  "WEAVE_AGENT_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.argv, 3)
			assert.Equal(t, []string{"sh", "-c"}, tt.argv[:2], "a pipeline needs a shell to read it")
			assert.Contains(t, tt.argv[2], tt.wantViewer)
			for _, export := range tt.wantExports {
				assert.Contains(t, tt.argv[2], export)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, tt.argv[2], tt.wantAbsent)
			}
		})
	}
}
