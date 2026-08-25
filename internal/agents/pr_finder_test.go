package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeGH(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))

	return dir
}

// TestGHPRFinder covers what gh's own output shape looks like: an array, one entry when a
// pull request is found and empty when there is none, never a bare null or object.
func TestGHPRFinder(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		wantURL   string
		wantErr   bool
		wantErrIn string
	}{
		{
			name:    "a pull request found",
			script:  `#!/bin/sh` + "\n" + `echo '[{"url":"https://github.com/harrisoncramer/weave/pull/1"}]'` + "\n",
			wantURL: "https://github.com/harrisoncramer/weave/pull/1",
		},
		{
			name:    "no pull request yet",
			script:  "#!/bin/sh\necho '[]'\n",
			wantURL: "",
		},
		{
			name:      "gh itself fails",
			script:    "#!/bin/sh\necho 'not a git repository' >&2\nexit 1\n",
			wantErr:   true,
			wantErrIn: "gh pr list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", fakeGH(t, tt.script))

			url, err := GHPRFinder{}.FindPR(t.Context(), t.TempDir(), "harry-fix")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrIn)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}
