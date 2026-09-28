package logread

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuns covers reading a script run's own output back by the name of the file it wrote.
func TestRuns(t *testing.T) {
	t.Parallel()

	output := []string{"+ git worktree add", "Preparing worktree", "error: port 5470 already allocated"}

	tests := []struct {
		name    string
		agentID string
		run     string
		pattern string
		from    int
		tail    string
		limit   int
		want    []string
		wantErr error
	}{
		{
			name:    "a name the agent never wrote",
			agentID: "a1",
			run:     "009-provision-setup.log",
			limit:   10,
			wantErr: ErrNoSuchRun,
		},
		{
			name:    "a name that leaves the agent's directory",
			agentID: "a1",
			run:     "../b2/001-provision-setup.log",
			limit:   10,
			wantErr: ErrNoSuchRun,
		},
		{
			name:    "an agent id that leaves the runs directory",
			agentID: "../outside",
			run:     "001-provision-setup.log",
			limit:   10,
			wantErr: ErrNoSuchRun,
		},
		{
			name:    "another agent's run",
			agentID: "b2",
			run:     "001-provision-setup.log",
			limit:   10,
			wantErr: ErrNoSuchRun,
		},
		{
			name:    "every line the run wrote, oldest first",
			agentID: "a1",
			run:     "001-provision-setup.log",
			limit:   10,
			want:    output,
		},
		{
			name:    "the limit keeps the newest",
			agentID: "a1",
			run:     "001-provision-setup.log",
			limit:   1,
			want:    output[2:],
		},
		{
			name:    "a read from an offset answers what came after it",
			agentID: "a1",
			run:     "001-provision-setup.log",
			from:    1,
			limit:   10,
			want:    output[1:],
		},
		{
			name:    "a pattern keeps only the lines it matches",
			agentID: "a1",
			run:     "001-provision-setup.log",
			pattern: "worktree",
			limit:   10,
			want:    output[:2],
		},
		{
			name:    "a half-written last line is left for the next read",
			agentID: "a1",
			run:     "001-provision-setup.log",
			tail:    "Container komrad-test",
			limit:   10,
			want:    output,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			dir := filepath.Join(root, "runs")
			body := ""
			for _, line := range output {
				body += line + "\n"
			}
			for _, agentDir := range []string{filepath.Join(dir, "a1"), filepath.Join(root, "outside")} {
				require.NoError(t, os.MkdirAll(agentDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(agentDir, "001-provision-setup.log"), []byte(body+tt.tail), 0o600))
			}

			runs := NewRuns(dir)
			run := Run{AgentID: tt.agentID, Name: tt.run}

			var (
				window *Window
				err    error
			)
			if tt.from > 0 {
				window, err = runs.Since(t.Context(), run, through(output, tt.from), Query{Pattern: patternFor(tt.pattern), Limit: tt.limit})
			} else {
				window, err = runs.Newest(t.Context(), run, Query{Pattern: patternFor(tt.pattern), Limit: tt.limit})
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, window.Lines)
			assert.Equal(t, through(output, len(output)), window.Offset)
		})
	}
}
