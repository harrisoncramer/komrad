package claude

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeSession(t *testing.T) {
	tests := []struct {
		name string
		log  string

		wantDir    string
		wantBranch string
		wantErrIs  error
	}{
		{
			name: "the directory and branch a conversation records",
			log: `{"type":"user","cwd":"/repo/theirs","gitBranch":"someones/work","message":{"content":"hello"}}` + "\n" +
				`{"type":"assistant","cwd":"/repo/theirs","gitBranch":"someones/work","message":{"content":[{"type":"text","text":"hi"}]}}`,
			wantDir:    "/repo/theirs",
			wantBranch: "someones/work",
		},
		{
			name: "the newest directory wins, since a conversation carried elsewhere keeps its log",
			log: `{"type":"user","cwd":"/repo/first","gitBranch":"first","message":{"content":"hello"}}` + "\n" +
				`{"type":"user","cwd":"/repo/second","gitBranch":"second","message":{"content":"still here"}}`,
			wantDir:    "/repo/second",
			wantBranch: "second",
		},
		{
			name: "lines carrying no directory are skipped rather than clearing one",
			log: `{"type":"user","cwd":"/repo/theirs","gitBranch":"someones/work","message":{"content":"hello"}}` + "\n" +
				`{"type":"mode","mode":"normal"}` + "\n" +
				`{"type":"file-history-snapshot"}`,
			wantDir:    "/repo/theirs",
			wantBranch: "someones/work",
		},
		{
			name:       "a conversation with no branch is still adoptable",
			log:        `{"type":"user","cwd":"/tmp/scratch","message":{"content":"hello"}}`,
			wantDir:    "/tmp/scratch",
			wantBranch: "",
		},
		{
			name:       "a detached checkout records HEAD, which is no branch to look for a pull request on",
			log:        `{"type":"user","cwd":"/repo/theirs","gitBranch":"HEAD","message":{"content":"hello"}}`,
			wantDir:    "/repo/theirs",
			wantBranch: "",
		},
		{
			name:      "a log recording no directory at all",
			log:       `{"type":"mode","mode":"normal"}`,
			wantErrIs: harness.ErrSessionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := t.TempDir()
			const sessionID = "019fb3e7-2cb6-7ab3-86a0-51a670a33140"

			folder := filepath.Join(projects, "-repo-theirs")
			require.NoError(t, os.MkdirAll(folder, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(folder, sessionID+".jsonl"), []byte(tt.log), 0o600))

			session, err := NewHarness(NewHarnessParams{ProjectsDir: projects}).DescribeSession(sessionID)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantDir, session.Dir)
			assert.Equal(t, tt.wantBranch, session.Branch)
		})
	}
}

func TestDescribeSessionWithoutALog(t *testing.T) {
	_, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).DescribeSession("nothing-here")
	require.ErrorIs(t, err, harness.ErrSessionNotFound)
}

func TestForksOnResume(t *testing.T) {
	assert.True(t, NewHarness(NewHarnessParams{}).ForksOnResume(),
		"Claude takes a session id on the command line, so an adoption can copy rather than take over")
}
