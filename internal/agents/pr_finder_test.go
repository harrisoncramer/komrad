package agents

import (
	"errors"
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
			wantErrIn: "not a git repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", fakeGH(t, tt.script))

			url, err := GHPRFinder{}.FindPR(t.Context(), t.TempDir(), "harry-fix")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrIn, "gh's own stderr rides along, not just the exit status")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}

// TestGHPRFinderClassifiesARateLimit covers the shape a real rate limit takes coming back
// through FindPR: gh's stderr has to survive being wrapped for isGHRateLimitError to see it.
func TestGHPRFinderClassifiesARateLimit(t *testing.T) {
	t.Setenv("PATH", fakeGH(t, "#!/bin/sh\necho 'API rate limit exceeded for installation ID 123.' >&2\nexit 1\n"))

	_, err := GHPRFinder{}.FindPR(t.Context(), t.TempDir(), "harry-fix")
	require.Error(t, err)
	assert.True(t, isGHRateLimitError(err), "gh's rate-limit wording is recognised")
}

// TestIsGHRateLimitError pins which gh failures are a rate limit worth backing off from and
// which are an ordinary error.
func TestIsGHRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil is not a rate limit",
			err:  nil,
			want: false,
		},
		{
			name: "the primary rate limit",
			err:  errors.New("gh pr list: API rate limit exceeded for installation ID 123."),
			want: true,
		},
		{
			name: "the secondary rate limit",
			err:  errors.New("You have exceeded a secondary rate limit. Please wait a few minutes."),
			want: true,
		},
		{
			name: "abuse detection wording",
			err:  errors.New("You have triggered an abuse detection mechanism."),
			want: true,
		},
		{
			name: "not a git repository is unrelated",
			err:  errors.New("not a git repository"),
			want: false,
		},
		{
			name: "a permission error is unrelated",
			err:  errors.New("HTTP 403: Resource not accessible"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isGHRateLimitError(tt.err))
		})
	}
}

// TestGHPRFinderIgnoresAClosedPullRequest covers the branch name a workflow reuses across
// attempts: the abandoned attempt leaves a closed pull request on that branch, and claiming it
// for the agent now on the branch retires an agent for work it never did.
func TestGHPRFinderIgnoresAClosedPullRequest(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\n" +
		`echo "$@" > ` + args + "\n" +
		`case "$*" in *"--state open"*) echo '[]' ;; *) echo '[{"url":"https://github.com/harrisoncramer/weave/pull/436"}]' ;; esac` + "\n"
	t.Setenv("PATH", fakeGH(t, script))

	url, err := GHPRFinder{}.FindPR(t.Context(), t.TempDir(), "fix/iss-435")
	require.NoError(t, err)
	assert.Empty(t, url, "a branch whose only pull request is closed has none to claim")

	asked, err := os.ReadFile(args)
	require.NoError(t, err)
	assert.Contains(t, string(asked), "--state open")
}
